import "server-only";
import createClient from "openapi-fetch";

import { env } from "@/shared/config/env";

import { toApiError } from "./errors";
import type { paths } from "./schema";

export type ApiResult<T> = { data?: T; error?: unknown; response: Response };

export type ApiClient = ReturnType<typeof createApiClient>;

/**
 * A typed client for the Tern API. Paths, parameters and responses all come from the OpenAPI
 * document, so a change to the API shows up here as a compile error.
 *
 * Pass the session token to call as a signed-in user, and the visitor's address (see
 * forwardedClientIp) for calls made on behalf of someone not signed in yet. Responses are never cached by the client:
 * pages decide what to cache.
 */
export function createApiClient(options: { token?: string; clientIp?: string } = {}) {
  const headers: Record<string, string> = {};
  if (options.token) headers.Authorization = `Bearer ${options.token}`;
  if (options.clientIp) headers["X-Real-IP"] = options.clientIp;
  return createClient<paths>({
    baseUrl: env.API_URL,
    headers,
    cache: "no-store",
  });
}

/**
 * Returns the data of a call, or throws the ApiError it failed with:
 *
 *   const endpoints = await unwrap(api.GET("/v1/endpoints"));
 */
export async function unwrap<T>(call: Promise<ApiResult<T>>): Promise<T> {
  const { data, error, response } = await call;
  if (error !== undefined || data === undefined) {
    // A 204 has no body, hence no data and no error: that is success with nothing to return.
    if (response.ok) return undefined as T;
    throw toApiError(response.status, error);
  }
  return data;
}
