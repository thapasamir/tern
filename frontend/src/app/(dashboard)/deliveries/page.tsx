import type { Metadata } from "next";
import Link from "next/link";
import { connection } from "next/server";
import { Suspense } from "react";

import { listDeliveries, listEndpointOptions } from "@/features/deliveries/api";
import { DeliveryFilters } from "@/features/deliveries/components/delivery-filters";
import { DeliveryTable } from "@/features/deliveries/components/delivery-table";
import { filtersToQuery, parseCursor, parseFilters } from "@/features/deliveries/filters";
import { Button } from "@/shared/ui/button";
import { PageHeader } from "@/shared/ui/page-header";

export const metadata: Metadata = { title: "Deliveries" };

export default function DeliveriesPage({ searchParams }: PageProps<"/deliveries">) {
  return (
    <>
      <PageHeader eyebrow="Deliveries" title="Delivery log" />
      <Suspense fallback={<p className="text-graphite">Loading deliveries…</p>}>
        <DeliveryLog searchParams={searchParams} />
      </Suspense>
    </>
  );
}

async function DeliveryLog({ searchParams }: { searchParams: PageProps<"/deliveries">["searchParams"] }) {
  const query = await searchParams;
  const filters = parseFilters(query);
  const cursor = parseCursor(query);
  await connection(); // the period is counted back from now, so this is rendered per request

  const [page, endpoints] = await Promise.all([listDeliveries(filters, new Date(), cursor), listEndpointOptions()]);

  return (
    <>
      <DeliveryFilters filters={filters} endpoints={endpoints} />
      {page.data.length === 0 ? (
        <div className="flex max-w-xl flex-col items-start gap-2 rounded-sm border border-dashed border-wing px-6 py-7">
          <h2 className="text-[22px] font-semibold tracking-tight">No deliveries match</h2>
          <p className="text-[15px] leading-relaxed text-graphite">
            Nothing was delivered in this period with these filters. Widen the period or clear a filter.
          </p>
        </div>
      ) : (
        <>
          <DeliveryTable deliveries={page.data} />
          <div className="flex flex-wrap items-center gap-x-6 gap-y-3 text-[15px] text-graphite">
            <span>
              Showing {page.data.length}, newest first{cursor ? " (older page)" : ""}.
            </span>
            {cursor ? (
              <Link
                href={`/deliveries${filtersToQuery(filters)}`}
                className="font-medium text-ink underline underline-offset-4"
              >
                Back to the newest
              </Link>
            ) : null}
            {page.next_cursor ? (
              <Button asChild variant="outline">
                <Link href={`/deliveries${filtersToQuery(filters, page.next_cursor)}`}>Older</Link>
              </Button>
            ) : null}
          </div>
        </>
      )}
    </>
  );
}
