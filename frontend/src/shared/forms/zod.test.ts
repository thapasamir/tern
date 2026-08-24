import { describe, expect, it } from "vitest";
import { z } from "zod";

import { firstErrorPerField } from "./zod";

describe("firstErrorPerField", () => {
  it("keeps only the first message of each field", () => {
    const schema = z.object({
      name: z.string().min(3, "too short").regex(/^a/, "must start with a"),
      age: z.number("not a number"),
    });
    const result = schema.safeParse({ name: "b", age: "x" });

    expect(firstErrorPerField(result.error!)).toEqual({ name: "too short", age: "not a number" });
  });
});
