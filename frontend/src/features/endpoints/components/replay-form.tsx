"use client";

import { useActionState } from "react";

import { FormField } from "@/shared/ui/form-field";
import { Select } from "@/shared/ui/select";
import { SubmitButton } from "@/shared/ui/submit-button";

import type { ReplayState } from "../actions";
import { replayWindows } from "../schemas";

/** Sends the endpoint's failed deliveries again, for use once whatever broke on its side is fixed. */
export function ReplayForm({
  action,
}: {
  action: (previous: ReplayState, formData: FormData) => Promise<ReplayState>;
}) {
  const [state, formAction] = useActionState(action, {});

  return (
    <form action={formAction} noValidate className="flex max-w-md flex-col gap-4">
      <FormField id="window" label="Replay failed deliveries from" error={state.fieldErrors?.window ?? state.error}>
        <Select id="window" name="window" defaultValue="86400">
          {replayWindows.map((w) => (
            <option key={w.value} value={w.value}>
              {w.label}
            </option>
          ))}
        </Select>
      </FormField>
      <div className="flex flex-wrap items-center gap-4">
        <SubmitButton variant="outline" pendingLabel="Replaying…">
          Replay
        </SubmitButton>
        {state.replayed !== undefined ? (
          <span role="status" className="text-[15px] font-medium text-ink">
            {summary(state.replayed, state.hasMore)}
          </span>
        ) : null}
      </div>
    </form>
  );
}

function summary(replayed: number, hasMore?: boolean): string {
  if (replayed === 0) return "Nothing to replay.";
  const base = `Queued ${replayed} ${replayed === 1 ? "delivery" : "deliveries"}.`;
  return hasMore ? `${base} More match: run it again to continue.` : base;
}
