import "server-only";
import { headers } from "next/headers";

/**
 * The address of the visitor whose request is being handled, as the nearest proxy saw it.
 *
 * The API limits sign-in attempts per client address. Called from here, every visitor would look
 * like this server, so the real address is passed along in X-Real-IP. We take the last entry of
 * X-Forwarded-For because that is the one our own proxy appended: earlier entries are whatever the
 * visitor chose to send. Without a proxy in front there is nothing to forward, and the API falls
 * back to the address of this server.
 */
export async function forwardedClientIp(): Promise<string | undefined> {
  const forwarded = (await headers()).get("x-forwarded-for");
  return forwarded?.split(",").at(-1)?.trim() || undefined;
}
