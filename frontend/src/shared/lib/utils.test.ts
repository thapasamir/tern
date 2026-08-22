import { describe, expect, it } from "vitest";

import { cn } from "./utils";

describe("cn", () => {
  it("joins class names and skips falsy ones", () => {
    expect(cn("a", false && "b", undefined, "c")).toBe("a c");
  });

  it("lets the last of two conflicting Tailwind classes win", () => {
    expect(cn("px-2 py-1", "px-4")).toBe("py-1 px-4");
  });
});
