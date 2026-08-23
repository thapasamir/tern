import { Suspense } from "react";

import { RequireAccount } from "@/features/auth/components/require-account";
import { UserMenu } from "@/features/auth/components/user-menu";
import { Masthead } from "@/shared/ui/masthead";

import { navItems } from "./nav";

/**
 * Everything here needs a signed-in user. The masthead and the page body each read the session, so
 * each sits behind its own boundary: the frame appears at once and the rest streams in.
 */
export default function DashboardLayout({ children }: LayoutProps<"/">) {
  return (
    <>
      <Masthead
        items={navItems}
        trailing={
          <Suspense>
            <UserMenu />
          </Suspense>
        }
      />
      <main className="mx-auto flex w-full max-w-[1280px] flex-col gap-10 px-5 pt-10 pb-20 sm:px-10">
        <Suspense>
          <RequireAccount>{children}</RequireAccount>
        </Suspense>
      </main>
    </>
  );
}
