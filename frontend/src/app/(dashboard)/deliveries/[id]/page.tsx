import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";

import { replayDeliveryAction } from "@/features/deliveries/actions";
import { getDelivery, getEventPayload } from "@/features/deliveries/api";
import { AttemptPath } from "@/features/deliveries/components/attempt-path";
import { AttemptsTable } from "@/features/deliveries/components/attempts-table";
import { headline } from "@/features/deliveries/headline";
import { formatDateTime, formatDuration } from "@/shared/lib/format";
import { StatusBadge } from "@/shared/ui/status-badge";
import { SubmitButton } from "@/shared/ui/submit-button";

export const metadata: Metadata = { title: "Delivery" };

export default function DeliveryPage({ params }: PageProps<"/deliveries/[id]">) {
  return (
    <Suspense fallback={<p className="text-graphite">Loading delivery…</p>}>
      <DeliveryDetail params={params} />
    </Suspense>
  );
}

async function DeliveryDetail({ params }: { params: PageProps<"/deliveries/[id]">["params"] }) {
  const { id } = await params;
  const delivery = await getDelivery(id);
  const payload = await getEventPayload(delivery.event_id);
  const last = delivery.attempts.at(-1);

  return (
    <>
      <nav aria-label="Breadcrumb" className="eyebrow">
        <Link href="/deliveries" className="underline-offset-4 hover:underline">
          Deliveries
        </Link>
        <span aria-hidden="true"> / </span>
        <span className="tracking-normal text-ink normal-case">{delivery.id}</span>
      </nav>

      <header className="flex flex-wrap items-start justify-between gap-x-10 gap-y-5">
        <div className="flex min-w-0 flex-col gap-3">
          <StatusBadge status={delivery.status} className="self-start" />
          <h1 className="text-4xl leading-tight font-semibold tracking-[-0.03em]">{headline(delivery)}</h1>
          <p className="data text-[15px] break-all text-graphite">
            {delivery.event_type} to {delivery.endpoint_url}
          </p>
        </div>
        <form action={replayDeliveryAction.bind(null, delivery.id)}>
          <SubmitButton pendingLabel="Replaying…">Replay delivery</SubmitButton>
        </form>
      </header>

      <dl className="grid max-w-3xl grid-cols-[max-content_1fr] gap-x-8 gap-y-2 text-[15px]">
        <dt className="eyebrow">Event id</dt>
        <dd className="data break-all">{delivery.event_id}</dd>
        <dt className="eyebrow">Created</dt>
        <dd className="data">{formatDateTime(delivery.created_at)}</dd>
        {delivery.completed_at ? (
          <>
            <dt className="eyebrow">{delivery.status === "failed" ? "Gave up" : "Finished"}</dt>
            <dd className="data">{formatDateTime(delivery.completed_at)}</dd>
          </>
        ) : null}
        {delivery.next_attempt_at ? (
          <>
            <dt className="eyebrow">Next attempt</dt>
            <dd className="data">{formatDateTime(delivery.next_attempt_at)}</dd>
          </>
        ) : null}
        {last ? (
          <>
            <dt className="eyebrow">Last response</dt>
            <dd className="data">
              {last.status_code ?? "no reply"} · {formatDuration(last.duration_ms)}
            </dd>
          </>
        ) : null}
        {delivery.replayed_from ? (
          <>
            <dt className="eyebrow">Replay of</dt>
            <dd className="data break-all">
              <Link href={`/deliveries/${delivery.replayed_from}`} className="underline underline-offset-4">
                {delivery.replayed_from}
              </Link>
            </dd>
          </>
        ) : null}
      </dl>

      {delivery.attempts.length > 0 ? (
        <section className="flex flex-col gap-4 border-t border-ink pt-6">
          <h2 className="eyebrow text-ink">Attempts along time</h2>
          <AttemptPath attempts={delivery.attempts} nextAttemptAt={delivery.next_attempt_at} />
          <p className="max-w-3xl text-sm text-graphite">
            Each cross is a failed attempt, a dot one that got through, a ring the one still to come. Distances are
            compressed so a 5 second wait and a 10 hour wait both stay visible; the labels give the real time since the
            first attempt.
          </p>
          <h2 className="eyebrow mt-4 text-ink">Every attempt</h2>
          <AttemptsTable attempts={delivery.attempts} />
        </section>
      ) : (
        <p className="max-w-xl rounded-sm border border-dashed border-wing px-6 py-5 text-[15px] text-graphite">
          No attempt has finished yet. It appears here as soon as one does.
        </p>
      )}

      <section className="grid gap-6 border-t border-ink pt-6 lg:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-3">
          <h2 className="eyebrow text-ink">What we sent</h2>
          {payload === null ? (
            <p className="text-[15px] text-graphite">The event has been deleted, so its payload is gone.</p>
          ) : (
            <pre className="data max-h-96 overflow-auto rounded-sm border border-rule bg-paper p-4 text-sm">
              {JSON.stringify(payload, null, 2)}
            </pre>
          )}
        </div>
        <div className="flex min-w-0 flex-col gap-3">
          <h2 className="eyebrow text-ink">What they answered</h2>
          {last ? (
            <>
              <p className="data text-[15px]">
                {last.status_code ?? "no reply"} · {formatDuration(last.duration_ms)}
                {last.error ? ` · ${last.error}` : ""}
              </p>
              <pre className="data max-h-96 overflow-auto rounded-sm border border-rule bg-paper p-4 text-sm break-all whitespace-pre-wrap">
                {last.response_body || "(empty body)"}
              </pre>
            </>
          ) : (
            <p className="text-[15px] text-graphite">Nothing yet.</p>
          )}
        </div>
      </section>

      <section className="flex flex-col items-start gap-3 border-t border-ink pt-6">
        <h2 className="eyebrow text-ink">Replay</h2>
        <p className="max-w-2xl text-[15px] text-graphite">
          Replaying makes a new delivery linked to this one and sends it again from the start. This record stays as it
          is.
        </p>
        <Link
          href={`/endpoints/${delivery.endpoint_id}`}
          className="text-[15px] font-medium underline underline-offset-4"
        >
          Replay all failed deliveries for this endpoint
        </Link>
      </section>
    </>
  );
}
