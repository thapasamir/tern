"use client";

import { usePathname } from "next/navigation";

import { type NavItem, NavTabs } from "./nav-tabs";

/** The tabs of the masthead, with the current section underlined and marked for screen readers. */
export function MastheadNav({ items }: { items: NavItem[] }) {
  return <NavTabs items={items} pathname={usePathname()} />;
}
