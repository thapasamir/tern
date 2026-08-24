import { describe, expect, it } from "vitest";

import { formatDateTime } from "./format";

describe("formatDateTime", () => {
  it("writes a UTC instant the same way wherever it runs", () => {
    expect(formatDateTime("2026-11-25T07:47:25Z")).toBe("25 Nov 2026, 07:47 UTC");
  });

  it("converts offsets to UTC", () => {
    expect(formatDateTime("2026-11-25T13:32:00+05:45")).toBe("25 Nov 2026, 07:47 UTC");
  });
});
