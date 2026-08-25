import { describe, expect, it } from "vitest";

import { DEFAULT_PERIOD, filtersToQuery, parseCursor, parseFilters, periodStart } from "./filters";

const ID = "01a21708-c636-7e48-944b-11f38b609f47";

describe("parseFilters", () => {
  it("defaults to the last 7 days with nothing else", () => {
    expect(parseFilters({})).toEqual({ period: DEFAULT_PERIOD });
  });

  it("reads every filter", () => {
    expect(parseFilters({ endpoint: ID, event: ID, status: "failed", period: "24h" })).toEqual({
      endpoint: ID,
      event: ID,
      status: "failed",
      period: "24h",
    });
  });

  it("ignores values it does not understand", () => {
    expect(parseFilters({ endpoint: "nope", event: "1", status: "weird", period: "forever" })).toEqual({
      period: DEFAULT_PERIOD,
    });
  });

  it("ignores repeated parameters and blanks", () => {
    expect(parseFilters({ status: ["failed", "pending"], endpoint: "  " })).toEqual({ period: DEFAULT_PERIOD });
  });
});

describe("filtersToQuery", () => {
  it("is empty for the defaults", () => {
    expect(filtersToQuery({ period: DEFAULT_PERIOD })).toBe("");
  });

  it("keeps filters and the page, and drops the default period", () => {
    expect(filtersToQuery({ status: "failed", period: DEFAULT_PERIOD }, "abc")).toBe("?status=failed&cursor=abc");
    expect(filtersToQuery({ endpoint: ID, period: "30d" })).toBe(`?endpoint=${ID}&period=30d`);
  });

  it("round-trips through parseFilters", () => {
    const filters = { endpoint: ID, event: ID, status: "pending", period: "24h" } as const;
    const query = Object.fromEntries(new URLSearchParams(filtersToQuery(filters)));
    expect(parseFilters(query)).toEqual(filters);
  });
});

describe("periodStart", () => {
  it("counts back from now", () => {
    const now = new Date("2026-08-14T12:00:00Z");
    expect(periodStart("24h", now).toISOString()).toBe("2026-08-13T12:00:00.000Z");
    expect(periodStart("7d", now).toISOString()).toBe("2026-08-07T12:00:00.000Z");
  });
});

describe("parseCursor", () => {
  it("accepts an id and nothing else", () => {
    expect(parseCursor({ cursor: ID })).toBe(ID);
    expect(parseCursor({ cursor: "notauuid" })).toBeUndefined();
    expect(parseCursor({ cursor: [ID, ID] })).toBeUndefined();
    expect(parseCursor({})).toBeUndefined();
  });
});
