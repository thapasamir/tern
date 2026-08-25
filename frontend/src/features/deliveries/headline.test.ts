import { describe, expect, it } from "vitest";

import { headline } from "./headline";

describe("headline", () => {
  it.each([
    [{ status: "succeeded", attempt_count: 1 }, "Delivered on the first attempt"],
    [{ status: "succeeded", attempt_count: 3 }, "Delivered on attempt 3"],
    [{ status: "failed", attempt_count: 1 }, "Failed on its only attempt"],
    [{ status: "failed", attempt_count: 8 }, "Failed after 8 attempts"],
    [{ status: "pending", attempt_count: 0 }, "Waiting to be sent"],
    [{ status: "pending", attempt_count: 1 }, "Waiting to retry after 1 failed attempt"],
    [{ status: "pending", attempt_count: 2 }, "Waiting to retry after 2 failed attempts"],
  ] as const)("%j", (delivery, text) => {
    expect(headline(delivery)).toBe(text);
  });
});
