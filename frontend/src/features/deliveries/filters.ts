/*
 * The delivery log's filters live in the URL, so a filtered view can be shared and survives a
 * refresh. This module turns a query string into filters and back, and decides what a bad value
 * means: it is ignored, never an error page, because anyone can edit a URL.
 */

export const statuses = ["pending", "succeeded", "failed"] as const;
export type DeliveryStatus = (typeof statuses)[number];

export const periods = [
  { value: "24h", label: "Last 24 hours", seconds: 24 * 3600 },
  { value: "7d", label: "Last 7 days", seconds: 7 * 24 * 3600 },
  { value: "30d", label: "Last 30 days", seconds: 30 * 24 * 3600 },
] as const;
export type Period = (typeof periods)[number]["value"];

export const DEFAULT_PERIOD: Period = "7d";

export type Filters = {
  endpoint?: string;
  status?: DeliveryStatus;
  period: Period;
  event?: string;
};

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

type Query = Record<string, string | string[] | undefined>;

function single(value: string | string[] | undefined): string | undefined {
  return typeof value === "string" ? value.trim() || undefined : undefined;
}

export function parseFilters(query: Query): Filters {
  const endpoint = single(query.endpoint);
  const event = single(query.event);
  const status = single(query.status);
  const period = single(query.period);
  return {
    endpoint: endpoint && UUID.test(endpoint) ? endpoint : undefined,
    event: event && UUID.test(event) ? event : undefined,
    status: statuses.find((s) => s === status),
    period: periods.find((p) => p.value === period)?.value ?? DEFAULT_PERIOD,
  };
}

/** The page to show, from `?cursor=`: only a real id is accepted, so a mangled link shows the newest page. */
export function parseCursor(query: Query): string | undefined {
  const cursor = single(query.cursor);
  return cursor && UUID.test(cursor) ? cursor : undefined;
}

/** The start of a period, counted back from `now`. */
export function periodStart(period: Period, now: Date): Date {
  const seconds = periods.find((p) => p.value === period)!.seconds;
  return new Date(now.getTime() - seconds * 1000);
}

/** The query string for a set of filters (and a page), leaving out whatever is at its default. */
export function filtersToQuery(filters: Filters, cursor?: string): string {
  const params = new URLSearchParams();
  if (filters.endpoint) params.set("endpoint", filters.endpoint);
  if (filters.status) params.set("status", filters.status);
  if (filters.period !== DEFAULT_PERIOD) params.set("period", filters.period);
  if (filters.event) params.set("event", filters.event);
  if (cursor) params.set("cursor", cursor);
  const text = params.toString();
  return text ? `?${text}` : "";
}
