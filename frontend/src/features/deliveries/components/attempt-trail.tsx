import { StatusGlyph } from "@/shared/ui/status-badge";

import type { DeliveryView } from "../api";

const MAX_SHOWN = 8;

/**
 * One glyph per attempt, in order: a delivery is only retried after a failure, so every attempt
 * before the last one failed. The last one is a dot if it got through, a cross if not, and a ring
 * stands for the attempt still to come.
 */
export function AttemptTrail({ delivery }: { delivery: Pick<DeliveryView, "status" | "attempt_count"> }) {
  const { status, attempt_count: count } = delivery;
  const failures = status === "succeeded" ? count - 1 : count;
  const glyphs = [
    ...Array<"failed">(Math.max(0, failures)).fill("failed"),
    ...(status === "succeeded" ? (["succeeded"] as const) : []),
    ...(status === "pending" ? (["pending"] as const) : []),
  ].slice(0, MAX_SHOWN);

  return (
    <span
      className="inline-flex items-center gap-1"
      role="img"
      aria-label={`${count} ${count === 1 ? "attempt" : "attempts"}`}
    >
      {glyphs.map((glyph, i) => (
        <StatusGlyph
          key={i}
          status={glyph}
          className={glyph === "failed" ? "text-bill" : glyph === "succeeded" ? "text-sea" : "text-ochre"}
        />
      ))}
    </span>
  );
}
