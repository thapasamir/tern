/**
 * Where to go after signing in, taken from a `?next=` parameter. Only a path on this site is
 * honoured: anything else (another host, a protocol-relative `//host`) would let a crafted link
 * send someone to a lookalike page right after they type their password.
 */
export function safeRedirectPath(next: string | null | undefined, fallback = "/"): string {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.includes("\\")) return fallback;
  return next;
}
