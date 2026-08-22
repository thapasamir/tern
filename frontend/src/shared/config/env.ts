import "server-only";
import { z } from "zod";

const schema = z.object({
  /** Where the Tern API listens. Only the server ever talks to it. */
  API_URL: z.url().default("http://localhost:8080"),
});

/** The server's environment, read once and checked: a bad value fails at startup, not mid-request. */
export const env = schema.parse(process.env);
