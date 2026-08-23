"use server";

import { redirect } from "next/navigation";
import { z, type ZodType } from "zod";

import { ApiError } from "@/shared/api/errors";
import { safeRedirectPath } from "@/shared/lib/safe-redirect";
import { clearSession, readSessionToken, writeSession } from "@/shared/session/session";

import { login, logout, signup, type LoginResult } from "./api";
import type { FormState } from "./form-state";
import { describeAuthError } from "./messages";
import { loginSchema, signupSchema } from "./schemas";

/*
 * Server actions behind the auth forms. They validate, call the API, store the session in a cookie
 * and redirect. A problem the visitor can fix comes back as FormState; anything else is thrown to
 * the error boundary.
 */

export async function loginAction(_previous: FormState, formData: FormData): Promise<FormState> {
  return authenticate(formData, loginSchema, login, "login", ["email"]);
}

export async function signupAction(_previous: FormState, formData: FormData): Promise<FormState> {
  return authenticate(formData, signupSchema, signup, "signup", ["email", "tenant_name"]);
}

export async function logoutAction(): Promise<void> {
  const token = await readSessionToken();
  if (token) {
    // Ending the session at the API is best effort: the cookie is dropped either way.
    await logout(token).catch(() => undefined);
  }
  await clearSession();
  redirect("/login");
}

/** The flow login and signup share. `keep` names the fields echoed back on failure. */
async function authenticate<Input>(
  formData: FormData,
  schema: ZodType<Input>,
  call: (input: Input) => Promise<LoginResult>,
  attempt: "login" | "signup",
  keep: string[],
): Promise<FormState> {
  const raw = Object.fromEntries(formData) as Record<string, string>;
  const values = Object.fromEntries(keep.map((name) => [name, raw[name] ?? ""]));

  const parsed = schema.safeParse(raw);
  if (!parsed.success) {
    const fieldErrors = firstErrorPerField(parsed.error);
    return { fieldErrors, values };
  }

  let result: LoginResult;
  try {
    result = await call(parsed.data);
  } catch (error) {
    if (error instanceof ApiError) return { error: describeAuthError(error, attempt), values };
    throw error;
  }

  await writeSession(result.session);
  redirect(safeRedirectPath(formData.get("next")?.toString()));
}

/** One message per field, the first: showing every rule a field broke at once is just noise. */
function firstErrorPerField(error: z.ZodError): Record<string, string> {
  const messages: Record<string, string> = {};
  for (const issue of error.issues) {
    messages[String(issue.path[0])] ??= issue.message;
  }
  return messages;
}
