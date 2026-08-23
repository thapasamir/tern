import "server-only";

import { createApiClient, unwrap } from "@/shared/api/client";
import { forwardedClientIp } from "@/shared/api/client-ip";
import type { components } from "@/shared/api/schema";

import type { LoginInput, SignupInput } from "./schemas";

export type Account = components["schemas"]["Account"];
export type LoginResult = components["schemas"]["LoginResult"];

/** Signing in and up happen before there is a session, on behalf of a visitor the API should tell apart. */
async function anonymousApi() {
  return createApiClient({ clientIp: await forwardedClientIp() });
}

export async function login(input: LoginInput): Promise<LoginResult> {
  const api = await anonymousApi();
  return unwrap(api.POST("/auth/login", { body: input }));
}

export async function signup(input: SignupInput): Promise<LoginResult> {
  const api = await anonymousApi();
  return unwrap(api.POST("/auth/signup", { body: input }));
}

export async function logout(token: string): Promise<void> {
  await unwrap(createApiClient({ token }).POST("/auth/logout"));
}

export async function fetchAccount(token: string): Promise<Account> {
  return unwrap(createApiClient({ token }).GET("/auth/me"));
}
