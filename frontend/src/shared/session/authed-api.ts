import "server-only";
import { redirect } from "next/navigation";

import { createApiClient } from "@/shared/api/client";

import { readSessionToken } from "./session";

/**
 * An API client that calls as the signed-in user. Every feature's `api.ts` starts from this, so a
 * missing session always ends the same way: the visitor is sent to sign in.
 */
export async function authedApi() {
  const token = await readSessionToken();
  if (!token) redirect("/login");
  return createApiClient({ token });
}
