import Link from "next/link";
import { type ReactNode, Suspense } from "react";

import { MastheadNav } from "./masthead-nav";
import { type NavItem, NavTabs } from "./nav-tabs";
import { TernMark } from "./tern-mark";

/**
 * The top of every signed-in page: the mark, the sections as text tabs, and a slot on the right
 * (the signed-in user). A masthead, not a sidebar: it reads like the heading of a page.
 */
export function Masthead({ items, trailing }: { items: NavItem[]; trailing?: ReactNode }) {
  return (
    <header className="border-b border-ink">
      <div className="mx-auto flex max-w-[1280px] flex-wrap items-center gap-x-10 px-5 sm:px-10">
        <Link href="/" className="flex items-center gap-2.5 py-3.5">
          <TernMark />
          <span className="text-2xl font-semibold tracking-tight">tern</span>
        </Link>
        {/* The current page is only known once the request is, so the tabs first appear unmarked. */}
        <Suspense fallback={<NavTabs items={items} />}>
          <MastheadNav items={items} />
        </Suspense>
        {trailing ? <div className="ml-auto py-2.5 text-[13px] text-graphite">{trailing}</div> : null}
      </div>
    </header>
  );
}
