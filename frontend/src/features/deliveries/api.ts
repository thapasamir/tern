import "server-only";
import { notFound } from "next/navigation";

import { ApiError } from "@/shared/api/errors";
import type { components } from "@/shared/api/schema";
import { callApi } from "@/shared/session/authed-api";

import { type Filters, periodStart } from "./filters";

export type DeliveryView = components["schemas"]["DeliveryView"];
export type DeliveryDetail = components["schemas"]["DeliveryDetail"];
export type Attempt = components["schemas"]["Attempt"];
export type DeliveryPage = components["schemas"]["DeliveryPage"];

const PAGE_SIZE = 25;

/** The delivery log for a set of filters, newest first. `now` anchors the period. */
export function listDeliveries(filters: Filters, now: Date, cursor?: string): Promise<DeliveryPage> {
  return callApi((api) =>
    api.GET("/v1/deliveries", {
      params: {
        query: {
          endpoint_id: filters.endpoint,
          event_id: filters.event,
          status: filters.status,
          since: periodStart(filters.period, now).toISOString(),
          limit: PAGE_SIZE,
          cursor,
        },
      },
    }),
  );
}

/** One delivery with every attempt, or the 404 page for an id that is not one of the tenant's. */
export async function getDelivery(id: string): Promise<DeliveryDetail> {
  try {
    return await callApi((api) => api.GET("/v1/deliveries/{deliveryID}", { params: { path: { deliveryID: id } } }));
  } catch (error) {
    if (error instanceof ApiError && (error.isNotFound || error.code === "invalid_request")) notFound();
    throw error;
  }
}

/** The payload of an event: what was sent to the endpoint. Null if the event is gone (retention). */
export async function getEventPayload(eventId: string): Promise<unknown> {
  try {
    const event = await callApi((api) => api.GET("/v1/events/{eventID}", { params: { path: { eventID: eventId } } }));
    return event.payload;
  } catch (error) {
    if (error instanceof ApiError && error.isNotFound) return null;
    throw error;
  }
}

/** The endpoints a log can be narrowed to: just enough to name them in a select. */
export async function listEndpointOptions(): Promise<{ id: string; url: string }[]> {
  const page = await callApi((api) => api.GET("/v1/endpoints", { params: { query: { limit: 100 } } }));
  return page.data.map(({ id, url }) => ({ id, url }));
}

/** Creates a new delivery repeating this one, and returns it. */
export function replayDelivery(id: string): Promise<components["schemas"]["Delivery"]> {
  return callApi((api) => api.POST("/v1/deliveries/{deliveryID}/replay", { params: { path: { deliveryID: id } } }));
}
