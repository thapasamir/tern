"use client";

import Link from "next/link";
import { useActionState } from "react";

import { FormField } from "@/shared/ui/form-field";
import { Input } from "@/shared/ui/input";
import { SubmitButton } from "@/shared/ui/submit-button";

import { signupAction } from "../actions";
import { FormError } from "./form-error";

export function SignupForm() {
  const [state, action] = useActionState(signupAction, {});

  return (
    <form action={action} noValidate className="flex flex-col gap-5">
      <FormError message={state.error} />
      <FormField
        id="tenant_name"
        label="Workspace"
        hint="Usually your company or product."
        error={state.fieldErrors?.tenant_name}
      >
        <Input
          id="tenant_name"
          name="tenant_name"
          autoComplete="organization"
          autoFocus
          defaultValue={state.values?.tenant_name}
          aria-invalid={Boolean(state.fieldErrors?.tenant_name)}
          aria-describedby="tenant_name-note"
        />
      </FormField>
      <FormField id="email" label="Email" error={state.fieldErrors?.email}>
        <Input
          id="email"
          name="email"
          type="email"
          autoComplete="email"
          defaultValue={state.values?.email}
          aria-invalid={Boolean(state.fieldErrors?.email)}
          aria-describedby="email-note"
        />
      </FormField>
      <FormField id="password" label="Password" hint="At least 8 characters." error={state.fieldErrors?.password}>
        <Input
          id="password"
          name="password"
          type="password"
          autoComplete="new-password"
          aria-invalid={Boolean(state.fieldErrors?.password)}
          aria-describedby="password-note"
        />
      </FormField>
      <SubmitButton pendingLabel="Creating account…">Create account</SubmitButton>
      <p className="text-sm text-graphite">
        Already have one?{" "}
        <Link href="/login" className="font-medium text-ink underline underline-offset-4">
          Sign in
        </Link>
      </p>
    </form>
  );
}
