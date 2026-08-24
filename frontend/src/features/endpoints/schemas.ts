import { z } from "zod";

/** The shape the API requires of an event type: lowercase words joined by `.`, `_` or `-`. */
const eventTypePattern = /^[a-z0-9]+(?:[._-][a-z0-9]+)*$/;

/**
 * Event types are typed into one box, separated by commas, spaces or new lines. Empty entries
 * and repeats are dropped. An empty result means "every event".
 */
export function parseEventTypes(text: string): string[] {
  return [...new Set(text.split(/[\s,]+/).filter(Boolean))];
}

export const endpointSchema = z.object({
  url: z
    .string()
    .trim()
    .min(1, "Enter the URL that should receive events.")
    .max(2048, "That URL is too long.")
    .pipe(z.url({ protocol: /^https?$/, error: "Enter a full URL starting with https://." })),
  description: z.string().trim().max(500, "Keep it under 500 characters."),
  event_types: z
    .string()
    .transform(parseEventTypes)
    .pipe(
      z
        .array(
          z
            .string()
            .max(128, "An event type can have 128 characters at most.")
            .regex(eventTypePattern, "Use lowercase words joined by . _ or -, like invoice.paid."),
        )
        .max(50, "Choose at most 50 event types, or leave it empty for all."),
    ),
});

export type EndpointInput = z.infer<typeof endpointSchema>;

/** How far back "replay failed deliveries" can reach, as the choices offered, in seconds. */
export const replayWindows = [
  { value: "3600", label: "the last hour" },
  { value: "86400", label: "the last 24 hours" },
  { value: "604800", label: "the last 7 days" },
  { value: "2592000", label: "the last 30 days" },
] as const;

export const replaySchema = z.object({
  window: z.enum(
    replayWindows.map((w) => w.value),
    { error: "Choose how far back to look." },
  ),
});
