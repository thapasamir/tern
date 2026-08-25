import { formatDuration, formatTimestamp } from "@/shared/lib/format";
import { StatusGlyph } from "@/shared/ui/status-badge";
import { Table, Td, Th, Tr } from "@/shared/ui/table";

import { attemptSucceeded } from "../attempt-path";
import type { Attempt } from "../api";

/** Every attempt, as text: the numbers behind the drawing. */
export function AttemptsTable({ attempts }: { attempts: Attempt[] }) {
  return (
    <Table>
      <thead>
        <tr>
          <Th>No.</Th>
          <Th>Started (UTC)</Th>
          <Th className="text-right">Took</Th>
          <Th className="text-right">Code</Th>
          <Th>Response</Th>
        </tr>
      </thead>
      <tbody>
        {attempts.map((a) => {
          const ok = attemptSucceeded(a.status_code);
          return (
            <Tr key={a.attempt_number}>
              <Td className="data whitespace-nowrap">
                <span className="inline-flex items-center gap-2">
                  <StatusGlyph status={ok ? "succeeded" : "failed"} className={ok ? "text-sea" : "text-bill"} />
                  {a.attempt_number}
                </span>
              </Td>
              <Td className="data text-sm whitespace-nowrap">{formatTimestamp(a.started_at)}</Td>
              <Td className="data text-right whitespace-nowrap">{formatDuration(a.duration_ms)}</Td>
              <Td className="data text-right">{a.status_code ?? "none"}</Td>
              <Td
                className="data max-w-[420px] truncate text-sm text-graphite"
                title={a.response_body ?? a.error ?? undefined}
              >
                {a.response_body || a.error || "–"}
              </Td>
            </Tr>
          );
        })}
      </tbody>
    </Table>
  );
}
