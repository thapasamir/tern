import { describe, expect, it } from "vitest";

import { endpointSchema, parseEventTypes, replaySchema } from "./schemas";

describe("parseEventTypes", () => {
  it("splits on commas, spaces and new lines, and drops blanks and repeats", () => {
    expect(parseEventTypes("invoice.paid, invoice.failed\n\ninvoice.paid  user.created,")).toEqual([
      "invoice.paid",
      "invoice.failed",
      "user.created",
    ]);
  });

  it("returns nothing for an empty box, which means every event", () => {
    expect(parseEventTypes("  \n ")).toEqual([]);
  });
});

describe("endpointSchema", () => {
  const valid = { url: "https://example.com/hooks", description: "", event_types: "" };

  it("accepts a URL with no description or event types", () => {
    expect(endpointSchema.parse(valid)).toEqual({ url: valid.url, description: "", event_types: [] });
  });

  it("parses the event types box into a list", () => {
    expect(endpointSchema.parse({ ...valid, event_types: "a.b c-d" }).event_types).toEqual(["a.b", "c-d"]);
  });

  it.each(["", "example.com", "ftp://example.com", "javascript:alert(1)"])("rejects the URL %j", (url) => {
    expect(endpointSchema.safeParse({ ...valid, url }).success).toBe(false);
  });

  it("rejects an event type the API would refuse", () => {
    for (const bad of ["Invoice.Paid", "invoice..paid", "-lead", "a".repeat(129)]) {
      expect(endpointSchema.safeParse({ ...valid, event_types: bad }).success, bad).toBe(false);
    }
  });

  it("limits the description to 500 characters", () => {
    expect(endpointSchema.safeParse({ ...valid, description: "x".repeat(501) }).success).toBe(false);
  });
});

describe("replaySchema", () => {
  it("accepts only the windows on offer", () => {
    expect(replaySchema.safeParse({ window: "86400" }).success).toBe(true);
    expect(replaySchema.safeParse({ window: "5" }).success).toBe(false);
  });
});
