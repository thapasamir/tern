import type { ZodType } from "zod";

import type { FormState } from "./form-state";
import { firstErrorPerField } from "./zod";

/**
 * Reads a submitted form through a schema. On success you get the typed data; on failure, a
 * ready-made FormState with a message per field and the typed values (`keep`) to put back, so the
 * action can return it as is.
 */
export function parseForm<T>(
  schema: ZodType<T>,
  formData: FormData,
  keep: string[],
): { ok: true; data: T } | { ok: false; state: FormState } {
  const raw = Object.fromEntries(formData) as Record<string, string>;
  const parsed = schema.safeParse(raw);
  if (parsed.success) return { ok: true, data: parsed.data };
  return {
    ok: false,
    state: {
      fieldErrors: firstErrorPerField(parsed.error),
      values: Object.fromEntries(keep.map((name) => [name, raw[name] ?? ""])),
    },
  };
}
