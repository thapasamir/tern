import { cn } from "@/shared/lib/utils";

/** The mark: a tern's head, a black cap and a red bill. */
export function TernMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" role="img" aria-label="Tern" className={cn("size-8", className)}>
      <circle cx="13" cy="16" r="9" className="fill-ink" />
      <polygon points="21,13 31,16.5 21,19" className="fill-bill-mark" />
      <circle cx="10.5" cy="14" r="1.4" className="fill-fog" />
    </svg>
  );
}
