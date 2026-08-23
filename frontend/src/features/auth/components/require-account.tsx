import type { ReactNode } from "react";

import { requireAccount } from "../account";

/**
 * Renders its children only for a signed-in visitor. It reads the session, so it must sit inside a
 * <Suspense> boundary. A layout is not re-run when navigating between its pages, so this is a first
 * line only: every page's own data calls go through authedApi, which refuses a missing session too.
 */
export async function RequireAccount({ children }: { children: ReactNode }) {
  await requireAccount();
  return children;
}
