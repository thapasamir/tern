import { NextResponse, type NextRequest } from "next/server";

import { SESSION_COOKIE } from "@/shared/session/cookie";

/**
 * A first, cheap check before any page renders: a visitor with no session cookie is sent to sign in,
 * and brought back to the page they asked for afterwards. It cannot tell whether the session is
 * still valid; pages and API calls check that. The matcher leaves out the sign-in pages and assets.
 */
export function proxy(request: NextRequest) {
  if (request.cookies.has(SESSION_COOKIE)) return NextResponse.next();

  const signIn = new URL("/login", request.url);
  const { pathname, search } = request.nextUrl;
  if (pathname !== "/") signIn.searchParams.set("next", pathname + search);
  return NextResponse.redirect(signIn);
}

export const config = {
  matcher: ["/((?!login|signup|_next/static|_next/image|icon.svg|styleguide).*)"],
};
