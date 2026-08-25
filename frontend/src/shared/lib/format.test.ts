import { describe, expect, it } from "vitest";

import { formatDateTime, formatDuration, formatElapsed, formatTimestamp } from "./format";

describe("formatDateTime", () => {
  it("writes a UTC instant the same way wherever it runs", () => {
    expect(formatDateTime("2026-11-25T07:47:25Z")).toBe("25 Nov 2026, 07:47 UTC");
  });

  it("converts offsets to UTC", () => {
    expect(formatDateTime("2026-11-25T13:32:00+05:45")).toBe("25 Nov 2026, 07:47 UTC");
  });
});

describe("formatTimestamp", () => {
  it("is compact and in UTC", () => {
    expect(formatTimestamp("2026-08-14T14:02:11Z")).toBe("14 Aug, 14:02:11");
    expect(formatTimestamp("2026-08-14T19:47:11+05:45")).toBe("14 Aug, 14:02:11");
  });
});

describe("formatDuration", () => {
  it.each([
    [97, "97 ms"],
    [999, "999 ms"],
    [1300, "1.3 s"],
    [2100, "2.1 s"],
    [10000, "10 s"],
    [10400, "10 s"],
  ])("%d ms is %s", (ms, text) => {
    expect(formatDuration(ms)).toBe(text);
  });
});

describe("formatElapsed", () => {
  it.each([
    [0, "+0"],
    [5, "+5 s"],
    [305, "+5 min"],
    [2105, "+35 min"],
    [3600, "+1 h"],
    [9305, "+2 h 35"],
    [99305, "+27 h 35"],
  ])("%d s is %s", (seconds, text) => {
    expect(formatElapsed(seconds)).toBe(text);
  });
});
