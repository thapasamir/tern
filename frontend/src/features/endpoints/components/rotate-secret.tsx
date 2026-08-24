"use client";

import { useActionState } from "react";

import { ConfirmAction } from "@/shared/ui/confirm-action";
import { SecretReveal } from "@/shared/ui/secret-reveal";

import type { RotatedState } from "../actions";

/** Replaces the signing secret. The old one stops working at once, so it asks first. */
export function RotateSecret({ action }: { action: (previous: RotatedState) => Promise<RotatedState> }) {
  const [state, formAction] = useActionState(action, {});

  if (state.secret) {
    return (
      <SecretReveal
        title="Your new signing secret"
        secret={state.secret}
        hint="The old secret no longer works. Update your server before the next event arrives. This is the only time it is shown."
      />
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {state.error ? (
        <p role="alert" className="text-sm font-medium text-bill">
          {state.error}
        </p>
      ) : null}
      <ConfirmAction
        action={formAction}
        variant="outline"
        label="Rotate secret"
        question="Events signed with the old secret will fail to verify. Rotate it?"
        confirmLabel="Rotate now"
        pendingLabel="Rotating…"
      />
    </div>
  );
}
