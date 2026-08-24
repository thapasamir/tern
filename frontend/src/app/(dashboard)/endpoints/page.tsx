import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";

import { listEndpoints } from "@/features/endpoints/api";
import { EndpointTable } from "@/features/endpoints/components/endpoint-table";
import { Button } from "@/shared/ui/button";
import { PageHeader } from "@/shared/ui/page-header";

export const metadata: Metadata = { title: "Endpoints" };

export default function EndpointsPage({ searchParams }: PageProps<"/endpoints">) {
  return (
    <>
      <PageHeader
        eyebrow="Endpoints"
        title="Where events go"
        actions={
          <Button asChild>
            <Link href="/endpoints/new">New endpoint</Link>
          </Button>
        }
      />
      <Suspense fallback={<p className="text-graphite">Loading endpoints…</p>}>
        <EndpointList searchParams={searchParams} />
      </Suspense>
    </>
  );
}

async function EndpointList({ searchParams }: { searchParams: PageProps<"/endpoints">["searchParams"] }) {
  const { cursor } = await searchParams;
  const page = await listEndpoints(typeof cursor === "string" ? cursor : undefined);

  if (page.data.length === 0) {
    return (
      <div className="flex max-w-xl flex-col items-start gap-3 rounded-sm border border-dashed border-wing px-6 py-7">
        <h2 className="text-[22px] font-semibold tracking-tight">No endpoints yet</h2>
        <p className="text-[15px] leading-relaxed text-graphite">
          An endpoint is a URL on your server. Add one and every event you send is delivered to it, signed and retried.
        </p>
        <Button asChild>
          <Link href="/endpoints/new">Add your first endpoint</Link>
        </Button>
      </div>
    );
  }

  return (
    <>
      <EndpointTable endpoints={page.data} />
      {page.next_cursor ? (
        <Button asChild variant="outline" className="self-start">
          <Link href={`/endpoints?cursor=${page.next_cursor}`}>Older endpoints</Link>
        </Button>
      ) : null}
    </>
  );
}
