import type { z } from "zod";

/** One message per field, the first: showing every rule a field broke at once is just noise. */
export function firstErrorPerField(error: z.ZodError): Record<string, string> {
  const messages: Record<string, string> = {};
  for (const issue of error.issues) {
    messages[String(issue.path[0])] ??= issue.message;
  }
  return messages;
}
