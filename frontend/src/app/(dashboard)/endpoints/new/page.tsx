import type { Metadata } from "next";

import { createEndpointAction } from "@/features/endpoints/actions";
import { EndpointForm } from "@/features/endpoints/components/endpoint-form";
import { PageHeader } from "@/shared/ui/page-header";

export const metadata: Metadata = { title: "New endpoint" };

export default function NewEndpointPage() {
  return (
    <>
      <PageHeader eyebrow="Endpoints" title="New endpoint" />
      <EndpointForm action={createEndpointAction} submitLabel="Create endpoint" pendingLabel="Creating…" />
    </>
  );
}
