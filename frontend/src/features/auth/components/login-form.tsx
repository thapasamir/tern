"use client";

import Link from "next/link";
import { useActionState } from "react";

import { FormField } from "@/shared/ui/form-field";
import { Input } from "@/shared/ui/input";
import { SubmitButton } from "@/shared/ui/submit-button";

import { loginAction } from "../actions";
import { FormError } from "./form-error";

/** `next` is where to go afterwards; the form passes it along untouched, the action checks it. */
export function LoginForm({ next }: { next?: string }) {
  const [state, action] = useActionState(loginAction, {});

  return (
    <form action={action} noValidate className="flex flex-col gap-5">
      {next ? <input type="hidden" name="next" value={next} /> : null}
      <FormError message={state.error} />
      <FormField id="email" label="Email" error={state.fieldErrors?.email}>
        <Input
          id="email"
          name="email"
          type="email"
          autoComplete="email"
          autoFocus
          defaultValue={state.values?.email}
          aria-invalid={Boolean(state.fieldErrors?.email)}
          aria-describedby="email-note"
        />
      </FormField>
      <FormField id="password" label="Password" error={state.fieldErrors?.password}>
        <Input
          id="password"
          name="password"
          type="password"
          autoComplete="current-password"
          aria-invalid={Boolean(state.fieldErrors?.password)}
          aria-describedby="password-note"
        />
      </FormField>
      <SubmitButton pendingLabel="Signing in…">Sign in</SubmitButton>
      <p className="text-sm text-graphite">
        New here?{" "}
        <Link href="/signup" className="font-medium text-ink underline underline-offset-4">
          Create an account
        </Link>
      </p>
    </form>
  );
}
