import { formatTimestamp } from "@/shared/lib/format";
import { StatusBadge } from "@/shared/ui/status-badge";

import type { DeliveryView } from "../api";

/** The status badge, with the line under it that says what is going on: a retry due, or how many tries it took. */
export function DeliveryStatus({
  delivery,
}: {
  delivery: Pick<DeliveryView, "status" | "attempt_count" | "next_attempt_at">;
}) {
  return (
    <div className="flex flex-col items-start gap-1">
      <StatusBadge status={delivery.status} />
      <span className="text-[13px] text-graphite">{note(delivery)}</span>
    </div>
  );
}

function note({
  status,
  attempt_count: count,
  next_attempt_at: next,
}: Pick<DeliveryView, "status" | "attempt_count" | "next_attempt_at">) {
  if (status === "pending")
    return next ? `retry at ${formatTimestamp(next)} UTC` : count === 0 ? "waiting to send" : "retrying soon";
  if (count <= 1) return status === "failed" ? "no retries left" : "first attempt";
  return status === "succeeded" ? `on attempt ${count}` : `after ${count} attempts`;
}
