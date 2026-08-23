import { describe, expect, it } from "vitest";

import { safeRedirectPath } from "./safe-redirect";

describe("safeRedirectPath", () => {
  it.each(["/", "/deliveries", "/deliveries?status=failed", "/endpoints/abc#top"])(
    "keeps the local path %s",
    (path) => {
      expect(safeRedirectPath(path)).toBe(path);
    },
  );

  it.each([
    ["an absolute URL", "https://evil.example/login"],
    ["a protocol-relative URL", "//evil.example"],
    ["a backslash trick", "/\\evil.example"],
    ["a bare word", "deliveries"],
    ["a script URL", "javascript:alert(1)"],
    ["an empty string", ""],
  ])("falls back for %s", (_name, next) => {
    expect(safeRedirectPath(next)).toBe("/");
  });

  it("falls back when there is nothing", () => {
    expect(safeRedirectPath(null)).toBe("/");
    expect(safeRedirectPath(undefined, "/deliveries")).toBe("/deliveries");
  });
});
