// Package delivery is the delivery feature: the record of every event on its way to every
// endpoint, and the API to read it. Like the other features it is organised as
//
//	model.go       Delivery, Attempt and the other domain types
//	repository.go  the persistence contract and its Postgres implementation (queries.sql, db/)
//	service.go     queueing deliveries and reading them back
//	controller.go  HTTP handlers      dto.go  request and response shapes      routes.go  wiring
//
// The part that actually makes the HTTP requests is larger and runs in the worker, not the API,
// so it lives in subpackages. They depend on this package, never the other way around:
//
//	dispatch  the processor: attempts, retries, recording, endpoint health
//	retry     the retry policy: backoff schedule, jitter, Retry-After
//	sender    the outbound HTTP client, hardened against calling internal addresses
package delivery
