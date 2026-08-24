import "server-only";
import { notFound } from "next/navigation";

import type { components } from "@/shared/api/schema";
import { ApiError } from "@/shared/api/errors";
import { callApi } from "@/shared/session/authed-api";

import type { EndpointInput } from "./schemas";

export type Endpoint = components["schemas"]["Endpoint"];
export type EndpointWithSecret = components["schemas"]["EndpointWithSecret"];
export type EndpointPage = components["schemas"]["EndpointPage"];

const PAGE_SIZE = 25;

export function listEndpoints(cursor?: string): Promise<EndpointPage> {
  return callApi((api) => api.GET("/v1/endpoints", { params: { query: { limit: PAGE_SIZE, cursor } } }));
}

/** One endpoint, or the 404 page: an id that does not exist, or is not even an id, is the same to a visitor. */
export async function getEndpoint(id: string): Promise<Endpoint> {
  try {
    return await callApi((api) => api.GET("/v1/endpoints/{endpointID}", { params: { path: { endpointID: id } } }));
  } catch (error) {
    if (error instanceof ApiError && (error.isNotFound || error.code === "invalid_request")) notFound();
    throw error;
  }
}

export function createEndpoint(input: EndpointInput): Promise<EndpointWithSecret> {
  return callApi((api) => api.POST("/v1/endpoints", { body: input }));
}

export function updateEndpoint(id: string, input: EndpointInput): Promise<Endpoint> {
  return callApi((api) =>
    api.PATCH("/v1/endpoints/{endpointID}", { params: { path: { endpointID: id } }, body: input }),
  );
}

export function deleteEndpoint(id: string): Promise<void> {
  return callApi((api) => api.DELETE("/v1/endpoints/{endpointID}", { params: { path: { endpointID: id } } }));
}

export function rotateSecret(id: string): Promise<EndpointWithSecret> {
  return callApi((api) =>
    api.POST("/v1/endpoints/{endpointID}/rotate-secret", { params: { path: { endpointID: id } } }),
  );
}

export function setEnabled(id: string, enabled: boolean): Promise<Endpoint> {
  const path = { params: { path: { endpointID: id } } };
  return callApi((api) =>
    enabled
      ? api.POST("/v1/endpoints/{endpointID}/enable", path)
      : api.POST("/v1/endpoints/{endpointID}/disable", path),
  );
}

/** Replays the endpoint's failed deliveries from `since` until now. */
export function replayFailed(id: string, since: Date): Promise<components["schemas"]["ReplayResult"]> {
  return callApi((api) =>
    api.POST("/v1/endpoints/{endpointID}/replay", {
      params: { path: { endpointID: id } },
      body: { since: since.toISOString() },
    }),
  );
}
