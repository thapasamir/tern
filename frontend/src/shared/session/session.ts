import "server-only";
import { cookies } from "next/headers";

import { SESSION_COOKIE } from "./cookie";

/**
 * The browser never sees the session token from JavaScript: it travels in an httpOnly cookie, which
 * only the server reads and attaches to API calls. A script injected into a page cannot steal it.
 */

/** The signed-in user's session token, or undefined when nobody is signed in. */
export async function readSessionToken(): Promise<string | undefined> {
  return (await cookies()).get(SESSION_COOKIE)?.value;
}

/** Remembers a session until it expires. Call from a server action or route handler. */
export async function writeSession(session: { token: string; expires_at: string }): Promise<void> {
  (await cookies()).set(SESSION_COOKIE, session.token, {
    httpOnly: true,
    sameSite: "lax", // sent on a link from elsewhere, so a bookmark still opens signed in; never on a cross-site POST
    secure: process.env.NODE_ENV === "production",
    path: "/",
    expires: new Date(session.expires_at),
  });
}

/** Forgets the session in this browser. */
export async function clearSession(): Promise<void> {
  (await cookies()).delete(SESSION_COOKIE);
}
