package dispatch

import (
	"context"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/features/delivery"
)

// reasonGone is recorded when an endpoint is disabled because it answered 410 Gone.
const reasonGone = "endpoint responded with HTTP 410 Gone"

// EndpointHealth lets the processor report how an endpoint is doing, without knowing how the
// endpoint feature stores or acts on it. It is implemented by endpoint.Service and wired in main.go.
type EndpointHealth interface {
	// RecordSuccess clears the endpoint's streak of failures.
	RecordSuccess(ctx context.Context, tenantID, endpointID uuid.UUID) error
	// RecordFailure counts a failed attempt and disables the endpoint after disableAfter in a row.
	// disabled is true only for the call that actually disabled it.
	RecordFailure(ctx context.Context, tenantID, endpointID uuid.UUID, disableAfter int) (disabled bool, err error)
	// DisableIfActive disables the endpoint at once, unless it already is. disabled is true only
	// for the call that actually disabled it.
	DisableIfActive(ctx context.Context, tenantID, endpointID uuid.UUID, reason string) (disabled bool, err error)
}

// reportHealth tells the endpoint how the attempt went. It runs after the attempt is recorded and
// is best effort: failing to update a counter must not fail the job and trigger a second send.
// If this is what disables the endpoint, its other pending deliveries are failed on the spot.
func (p *Processor) reportHealth(ctx context.Context, w delivery.Work, v verdict) {
	disabled, err := p.recordHealth(ctx, w, v.health)
	if err != nil {
		p.log.Error("updating endpoint health failed", "endpoint_id", w.EndpointID, "error", err)
		return
	}
	if disabled {
		p.failPendingDeliveries(ctx, w.EndpointID)
	}
}

// recordHealth passes the attempt's health action on to the endpoint feature and reports whether
// that disabled the endpoint.
func (p *Processor) recordHealth(ctx context.Context, w delivery.Work, action healthAction) (disabled bool, err error) {
	switch action {
	case healthSuccess:
		return false, p.health.RecordSuccess(ctx, w.TenantID, w.EndpointID)
	case healthFailure:
		return p.health.RecordFailure(ctx, w.TenantID, w.EndpointID, p.cfg.AutoDisableAfter)
	case healthGone:
		return p.health.DisableIfActive(ctx, w.TenantID, w.EndpointID, reasonGone)
	default:
		return false, nil
	}
}

// failPendingDeliveries ends the deliveries still waiting on an endpoint that was just disabled,
// rather than leaving them to retry against an endpoint that will refuse them.
func (p *Processor) failPendingDeliveries(ctx context.Context, endpointID uuid.UUID) {
	failed, err := p.repo.FailPendingForEndpoint(ctx, endpointID, errEndpointDisabled.Error(), p.now())
	if err != nil {
		p.log.Error("failing the pending deliveries of a disabled endpoint failed",
			"endpoint_id", endpointID, "error", err)
		return
	}
	p.log.Warn("endpoint disabled", "endpoint_id", endpointID, "pending_deliveries_failed", failed)
}
