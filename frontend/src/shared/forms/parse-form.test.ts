import { describe, expect, it } from "vitest";
import { z } from "zod";

import { parseForm } from "./parse-form";

const schema = z.object({ name: z.string().min(1, "Name it."), note: z.string() });

function form(fields: Record<string, string>) {
  const data = new FormData();
  for (const [key, value] of Object.entries(fields)) data.set(key, value);
  return data;
}

describe("parseForm", () => {
  it("returns the typed data of a valid form", () => {
    expect(parseForm(schema, form({ name: "a", note: "b" }), ["name"])).toEqual({
      ok: true,
      data: { name: "a", note: "b" },
    });
  });

  it("returns field errors and the values to keep for an invalid one", () => {
    const result = parseForm(schema, form({ name: "", note: "secret" }), ["name"]);

    expect(result).toEqual({ ok: false, state: { fieldErrors: { name: "Name it." }, values: { name: "" } } });
  });
});
