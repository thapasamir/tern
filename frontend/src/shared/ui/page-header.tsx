import type { ReactNode } from "react";

/** The head of a screen: a small caps eyebrow, the title, and room on the right for actions. */
export function PageHeader({ eyebrow, title, actions }: { eyebrow: string; title: string; actions?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-x-10 gap-y-4">
      <div className="flex flex-col gap-2">
        <span className="eyebrow">{eyebrow}</span>
        <h1 className="text-5xl leading-none font-semibold tracking-[-0.03em]">{title}</h1>
      </div>
      {actions}
    </div>
  );
}
