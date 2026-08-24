"use client";

import Link from "next/link";
import { useActionState } from "react";

import type { FormState } from "@/shared/forms/form-state";
import { Button } from "@/shared/ui/button";
import { FormField } from "@/shared/ui/form-field";
import { Input } from "@/shared/ui/input";
import { SecretReveal } from "@/shared/ui/secret-reveal";
import { SubmitButton } from "@/shared/ui/submit-button";
import { Textarea } from "@/shared/ui/textarea";

export type EndpointFormState = FormState<{ created?: { id: string; secret: string }; saved?: boolean }>;

type Props = {
  action: (previous: EndpointFormState, formData: FormData) => Promise<EndpointFormState>;
  /** The endpoint being edited; leave out to create one. */
  initial?: { url: string; description: string; event_types: string[] };
  submitLabel: string;
  pendingLabel: string;
};

/**
 * The form to create an endpoint or change one. Creating ends in the signing secret, shown once in
 * place of the form; editing ends in a short "Saved".
 */
export function EndpointForm({ action, initial, submitLabel, pendingLabel }: Props) {
  const [state, formAction] = useActionState(action, {});

  if (state.created) {
    return (
      <div className="flex flex-col gap-6">
        <SecretReveal
          title="Your signing secret"
          secret={state.created.secret}
          hint="Use it to check the signature on every event we send. It is shown only now: copy it before you leave."
        />
        <div className="flex flex-wrap gap-3">
          <Button asChild>
            <Link href={`/endpoints/${state.created.id}`}>Open the endpoint</Link>
          </Button>
          <Button asChild variant="outline">
            <Link href="/endpoints">All endpoints</Link>
          </Button>
        </div>
      </div>
    );
  }

  const value = (name: "url" | "description" | "event_types") =>
    state.values?.[name] ?? (name === "event_types" ? initial?.event_types.join("\n") : initial?.[name]) ?? "";
  const invalid = (name: string) => Boolean(state.fieldErrors?.[name]);

  return (
    <form action={formAction} noValidate className="flex max-w-2xl flex-col gap-5">
      {state.error ? (
        <p
          role="alert"
          className="rounded-sm border border-bill bg-bill-tint px-3.5 py-3 text-sm font-medium text-bill"
        >
          {state.error}
        </p>
      ) : null}
      <FormField id="url" label="URL" error={state.fieldErrors?.url} hint="Where events are sent, over HTTPS.">
        <Input
          id="url"
          name="url"
          type="url"
          inputMode="url"
          autoComplete="off"
          autoFocus={!initial}
          placeholder="https://example.com/webhooks"
          className="data"
          defaultValue={value("url")}
          aria-invalid={invalid("url")}
          aria-describedby="url-note"
        />
      </FormField>
      <FormField id="description" label="Description" error={state.fieldErrors?.description}>
        <Input
          id="description"
          name="description"
          autoComplete="off"
          defaultValue={value("description")}
          aria-invalid={invalid("description")}
          aria-describedby="description-note"
        />
      </FormField>
      <FormField
        id="event_types"
        label="Event types"
        error={state.fieldErrors?.event_types}
        hint="One per line or separated by commas, like invoice.paid. Leave empty to receive every event."
      >
        <Textarea
          id="event_types"
          name="event_types"
          className="data"
          defaultValue={value("event_types")}
          aria-invalid={invalid("event_types")}
          aria-describedby="event_types-note"
        />
      </FormField>
      <div className="flex flex-wrap items-center gap-4">
        <SubmitButton pendingLabel={pendingLabel}>{submitLabel}</SubmitButton>
        {state.saved ? (
          <span role="status" className="text-[15px] font-medium text-sea">
            Saved.
          </span>
        ) : null}
      </div>
    </form>
  );
}
