/**
 * The name of the cookie that holds the session token. It lives apart from the code that reads and
 * writes it so the proxy, which runs before pages and cannot import server-only modules, can check
 * for it too.
 */
export const SESSION_COOKIE = "tern_session";
