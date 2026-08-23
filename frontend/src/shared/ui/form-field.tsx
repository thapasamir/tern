import type { ReactNode } from "react";

import { Label } from "./label";

/**
 * A labelled form control with its hint and error. The control goes in as the child and must have
 * the same `id`; give it `aria-invalid` and `aria-describedby={`${id}-note`}` so the note is read
 * out with it.
 */
export function FormField({
  id,
  label,
  hint,
  error,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  children: ReactNode;
}) {
  const note = error ?? hint;
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {note ? (
        <p id={`${id}-note`} className={error ? "text-sm font-medium text-bill" : "text-sm text-wing"}>
          {note}
        </p>
      ) : null}
    </div>
  );
}
