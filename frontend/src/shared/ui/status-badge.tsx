import * as React from "react";

import { cn } from "@/shared/lib/utils";

/**
 * Status is a shape before it is a colour: a filled dot, a cross, a ring. Colour backs the shape
 * up and never carries the meaning alone, so the badge reads in greyscale and for colour-blind
 * people.
 */
export type Status = "succeeded" | "failed" | "pending" | "active" | "disabled";

const glyphs: Record<Status, React.ReactNode> = {
  succeeded: <circle cx="6" cy="6" r="5" fill="currentColor" />,
  failed: <path d="M2 2 L10 10 M10 2 L2 10" stroke="currentColor" strokeWidth="2" fill="none" />,
  pending: <circle cx="6" cy="6" r="4.2" fill="none" stroke="currentColor" strokeWidth="1.8" />,
  active: <circle cx="6" cy="6" r="5" fill="currentColor" />,
  disabled: (
    <>
      <circle cx="6" cy="6" r="4.6" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path d="M2.8 9.2 L9.2 2.8" stroke="currentColor" strokeWidth="1.6" />
    </>
  ),
};

const labels: Record<Status, string> = {
  succeeded: "Succeeded",
  failed: "Failed",
  pending: "Pending",
  active: "Active",
  disabled: "Disabled",
};

const looks: Record<Status, string> = {
  succeeded: "bg-sea-tint text-sea",
  failed: "bg-bill-tint text-bill",
  pending: "bg-ochre-tint text-ochre",
  active: "border border-ink text-ink",
  disabled: "border border-bill text-bill",
};

/** Just the shape, for table rows where the word sits beside it. Decorative: the word is the label. */
function StatusGlyph({ status, className }: { status: Status; className?: string }) {
  return (
    <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true" className={cn("shrink-0", className)}>
      {glyphs[status]}
    </svg>
  );
}

function StatusBadge({ status, label, className }: { status: Status; label?: string; className?: string }) {
  return (
    <span
      data-slot="status-badge"
      className={cn(
        "inline-flex h-7 items-center gap-2 rounded-sm px-3 pl-2.5 font-mono text-xs font-semibold tracking-[0.06em] uppercase",
        looks[status],
        className,
      )}
    >
      <StatusGlyph status={status} />
      {label ?? labels[status]}
    </span>
  );
}

export { StatusBadge, StatusGlyph, labels as statusLabels };
