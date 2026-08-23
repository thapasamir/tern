import "server-only";
import { redirect } from "next/navigation";
import { cache } from "react";

import { ApiError } from "@/shared/api/errors";
import { readSessionToken } from "@/shared/session/session";

import { safeRedirectPath } from "@/shared/lib/safe-redirect";
import { type Account, fetchAccount } from "./api";

/**
 * Who is signed in, or null. A session the API no longer knows (expired, or ended elsewhere) counts
 * as nobody. Memoised per request, so the masthead and the page can both ask without two calls.
 */
export const getAccount = cache(async (): Promise<Account | null> => {
  const token = await readSessionToken();
  if (!token) return null;
  try {
    return await fetchAccount(token);
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) return null;
    throw error;
  }
});

/** The signed-in account; anyone else is sent to sign in and, after that, back to where they were. */
export async function requireAccount(): Promise<Account> {
  const account = await getAccount();
  if (!account) redirect("/login");
  return account;
}

/** For the sign-in and sign-up pages: someone already signed in has no use for them. */
export async function redirectIfSignedIn(next?: string): Promise<void> {
  if (await getAccount()) redirect(safeRedirectPath(next));
}
