import Link from "next/link";

import { formatDateTime } from "@/shared/lib/format";
import { StatusBadge } from "@/shared/ui/status-badge";
import { Table, Td, Th, Tr } from "@/shared/ui/table";

import type { Endpoint } from "../api";
import { EventTypes } from "./event-types";

export function EndpointTable({ endpoints }: { endpoints: Endpoint[] }) {
  return (
    <Table>
      <thead>
        <tr>
          <Th>Endpoint</Th>
          <Th>Status</Th>
          <Th>Receives</Th>
          <Th className="text-right">Failing in a row</Th>
          <Th>Created</Th>
        </tr>
      </thead>
      <tbody>
        {endpoints.map((endpoint) => (
          <Tr key={endpoint.id}>
            <Td className="max-w-[360px]">
              <Link
                href={`/endpoints/${endpoint.id}`}
                className="data block truncate font-medium underline-offset-4 hover:underline"
              >
                {endpoint.url}
              </Link>
              {endpoint.description ? (
                <span className="block truncate text-sm text-graphite">{endpoint.description}</span>
              ) : null}
            </Td>
            <Td>
              <StatusBadge status={endpoint.status} />
            </Td>
            <Td>
              <EventTypes types={endpoint.event_types} />
            </Td>
            <Td className="data text-right">{endpoint.consecutive_failures}</Td>
            <Td className="data text-sm whitespace-nowrap text-graphite">{formatDateTime(endpoint.created_at)}</Td>
          </Tr>
        ))}
      </tbody>
    </Table>
  );
}
