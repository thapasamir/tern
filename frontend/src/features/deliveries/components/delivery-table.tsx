import Link from "next/link";

import { formatDuration, formatTimestamp } from "@/shared/lib/format";
import { Table, Td, Th, Tr } from "@/shared/ui/table";

import type { DeliveryView } from "../api";
import { AttemptTrail } from "./attempt-trail";
import { DeliveryStatus } from "./delivery-status";

/** The delivery log: one row per delivery, newest first. Times are UTC. */
export function DeliveryTable({ deliveries }: { deliveries: DeliveryView[] }) {
  return (
    <Table className="min-w-[860px]">
      <thead>
        <tr>
          <Th>Status</Th>
          <Th>When (UTC)</Th>
          <Th>Event</Th>
          <Th>Endpoint</Th>
          <Th>Attempts</Th>
          <Th className="text-right">Code</Th>
          <Th className="text-right">Took</Th>
        </tr>
      </thead>
      <tbody>
        {deliveries.map((d) => (
          <Tr key={d.id}>
            <Td>
              <DeliveryStatus delivery={d} />
            </Td>
            <Td className="data text-sm whitespace-nowrap">
              <Link href={`/deliveries/${d.id}`} className="underline-offset-4 hover:underline">
                {formatTimestamp(d.created_at)}
              </Link>
            </Td>
            <Td className="data text-sm">{d.event_type}</Td>
            <Td className="data max-w-[260px] truncate text-sm text-graphite" title={d.endpoint_url}>
              {d.endpoint_url}
            </Td>
            <Td>
              <AttemptTrail delivery={d} />
            </Td>
            <Td className="data text-right">{d.last_attempt?.status_code ?? (d.last_attempt ? "none" : "–")}</Td>
            <Td className="data text-right whitespace-nowrap">
              {d.last_attempt ? formatDuration(d.last_attempt.duration_ms) : "–"}
            </Td>
          </Tr>
        ))}
      </tbody>
    </Table>
  );
}
