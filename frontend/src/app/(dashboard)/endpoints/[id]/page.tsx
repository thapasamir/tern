import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";

import {
  deleteEndpointAction,
  replayFailedAction,
  rotateSecretAction,
  setEnabledAction,
  updateEndpointAction,
} from "@/features/endpoints/actions";
import { getEndpoint } from "@/features/endpoints/api";
import { EndpointForm } from "@/features/endpoints/components/endpoint-form";
import { ReplayForm } from "@/features/endpoints/components/replay-form";
import { RotateSecret } from "@/features/endpoints/components/rotate-secret";
import { formatDateTime } from "@/shared/lib/format";
import { ConfirmAction } from "@/shared/ui/confirm-action";
import { PageHeader } from "@/shared/ui/page-header";
import { StatusBadge } from "@/shared/ui/status-badge";
import { SubmitButton } from "@/shared/ui/submit-button";

export const metadata: Metadata = { title: "Endpoint" };

export default function EndpointPage({ params }: PageProps<"/endpoints/[id]">) {
  return (
    <Suspense fallback={<p className="text-graphite">Loading endpoint…</p>}>
      <EndpointDetail params={params} />
    </Suspense>
  );
}

async function EndpointDetail({ params }: { params: PageProps<"/endpoints/[id]">["params"] }) {
  const { id } = await params;
  const endpoint = await getEndpoint(id);
  const active = endpoint.status === "active";

  return (
    <>
      <PageHeader
        eyebrow="Endpoint"
        title="Settings"
        actions={
          <form action={setEnabledAction.bind(null, id, !active)}>
            <SubmitButton variant="outline" pendingLabel={active ? "Disabling…" : "Enabling…"}>
              {active ? "Disable" : "Enable"}
            </SubmitButton>
          </form>
        }
      />

      <section aria-label="Status" className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
          <StatusBadge status={endpoint.status} />
          <span className="data text-[15px] break-all">{endpoint.url}</span>
        </div>
        {endpoint.disabled_reason ? (
          <p className="max-w-2xl rounded-sm border border-bill bg-bill-tint px-3.5 py-3 text-sm font-medium text-bill">
            Disabled{endpoint.disabled_at ? ` on ${formatDateTime(endpoint.disabled_at)}` : ""}:{" "}
            {endpoint.disabled_reason}. Fix the problem on your side, then enable it again.
          </p>
        ) : null}
        <dl className="grid max-w-2xl grid-cols-[max-content_1fr] gap-x-8 gap-y-2 text-[15px]">
          <dt className="eyebrow">Id</dt>
          <dd className="data break-all">{endpoint.id}</dd>
          <dt className="eyebrow">Created</dt>
          <dd className="data">{formatDateTime(endpoint.created_at)}</dd>
          <dt className="eyebrow">Failing in a row</dt>
          <dd className="data">{endpoint.consecutive_failures}</dd>
        </dl>
      </section>

      <Section title="Details">
        <EndpointForm
          action={updateEndpointAction.bind(null, id)}
          initial={endpoint}
          submitLabel="Save changes"
          pendingLabel="Saving…"
        />
      </Section>

      <Section title="Signing secret">
        <p className="max-w-2xl text-[15px] text-graphite">
          Every event is signed with this endpoint&apos;s secret. It was shown once, when the endpoint was created. If
          it is lost or leaked, rotate it.
        </p>
        <RotateSecret action={rotateSecretAction.bind(null, id)} />
      </Section>

      <Section title="Replay">
        <p className="max-w-2xl text-[15px] text-graphite">
          After you have fixed a problem on your server, send the deliveries that failed in the meantime again. Ones
          already replayed are skipped.
        </p>
        <ReplayForm action={replayFailedAction.bind(null, id)} />
      </Section>

      <Section title="Delete">
        <p className="max-w-2xl text-[15px] text-graphite">
          Deleting stops all deliveries to this URL and removes its history. It cannot be undone.
        </p>
        <ConfirmAction
          action={deleteEndpointAction.bind(null, id)}
          label="Delete endpoint"
          question="Delete this endpoint and its history?"
          confirmLabel="Delete for good"
          pendingLabel="Deleting…"
        />
      </Section>

      <Link href="/endpoints" className="self-start text-[15px] font-medium underline underline-offset-4">
        Back to endpoints
      </Link>
    </>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col items-start gap-4 border-t border-ink pt-6">
      <h2 className="eyebrow text-ink">{title}</h2>
      {children}
    </section>
  );
}
