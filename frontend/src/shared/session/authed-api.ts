import "server-only";
import { redirect } from "next/navigation";

import { type ApiClient, type ApiResult, createApiClient, unwrap } from "@/shared/api/client";
import { ApiError } from "@/shared/api/errors";

import { readSessionToken } from "./session";

/**
 * Calls the API as the signed-in user and returns the data, or throws the ApiError it failed with:
 *
 *   const page = await callApi((api) => api.GET("/v1/endpoints"));
 *
 * Every feature's `api.ts` goes through this, so a missing or rejected session always ends the same
 * way: the visitor is sent to sign in, wherever in the app they were.
 */
export async function callApi<T>(call: (api: ApiClient) => Promise<ApiResult<T>>): Promise<T> {
  const token = await readSessionToken();
  if (!token) redirect("/login");
  try {
    return await unwrap(call(createApiClient({ token })));
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) redirect("/login");
    throw error;
  }
}
