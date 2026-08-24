"use client";

import { Button } from "@/shared/ui/button";

/** What a signed-in page shows when something unexpected broke: a plain sentence and a way to retry. */
export default function DashboardError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <div
      role="alert"
      className="flex max-w-xl flex-col items-start gap-3 rounded-sm border border-bill bg-bill-tint px-6 py-7"
    >
      <h1 className="text-[22px] font-semibold tracking-tight text-bill">Something went wrong</h1>
      <p className="text-[15px] leading-relaxed text-ink">
        We couldn&apos;t load this page. Try again, and if it keeps happening tell us the code below.
      </p>
      {error.digest ? <code className="data text-sm">{error.digest}</code> : null}
      <Button type="button" onClick={reset}>
        Try again
      </Button>
    </div>
  );
}
