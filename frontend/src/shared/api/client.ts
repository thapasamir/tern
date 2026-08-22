import "server-only";
import createClient from "openapi-fetch";

import { env } from "@/shared/config/env";

import { toApiError } from "./errors";
import type { paths } from "./schema";

type Result<T> = { data?: T; error?: unknown; response: Response };

/**
 * A typed client for the Tern API. Paths, parameters and responses all come from the OpenAPI
 * document, so a change to the API shows up here as a compile error.
 *
 * Pass the session token to call as a signed-in user. Responses are never cached by the client:
 * pages decide what to cache.
 */
export function createApiClient(options: { token?: string } = {}) {
  return createClient<paths>({
    baseUrl: env.API_URL,
    headers: options.token ? { Authorization: `Bearer ${options.token}` } : undefined,
    cache: "no-store",
  });
}

/**
 * Returns the data of a call, or throws the ApiError it failed with:
 *
 *   const endpoints = await unwrap(api.GET("/v1/endpoints"));
 */
export async function unwrap<T>(call: Promise<Result<T>>): Promise<T> {
  const { data, error, response } = await call;
  if (error !== undefined || data === undefined) {
    // A 204 has no body, hence no data and no error: that is success with nothing to return.
    if (response.ok) return undefined as T;
    throw toApiError(response.status, error);
  }
  return data;
}
