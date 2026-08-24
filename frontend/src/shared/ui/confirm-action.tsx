"use client";

import { useState } from "react";

import { Button } from "./button";
import { SubmitButton } from "./submit-button";

/**
 * A button for something that is hard to undo. The first click only asks; a second, separate
 * button does it. `action` is a server action (bind any arguments before passing it in).
 */
export function ConfirmAction({
  action,
  label,
  question,
  confirmLabel,
  pendingLabel,
  variant = "destructive",
}: {
  action: (formData: FormData) => void | Promise<void>;
  label: string;
  question: string;
  confirmLabel: string;
  pendingLabel: string;
  variant?: "destructive" | "outline";
}) {
  const [asking, setAsking] = useState(false);

  if (!asking) {
    return (
      <Button type="button" variant={variant} onClick={() => setAsking(true)}>
        {label}
      </Button>
    );
  }

  return (
    <form action={action} className="flex flex-wrap items-center gap-3">
      <p role="alert" className="text-[15px] font-medium text-ink">
        {question}
      </p>
      <SubmitButton variant={variant} pendingLabel={pendingLabel}>
        {confirmLabel}
      </SubmitButton>
      <Button type="button" variant="quiet" onClick={() => setAsking(false)}>
        Cancel
      </Button>
    </form>
  );
}
