import Link from "next/link";

import { cn } from "@/shared/lib/utils";

export type NavItem = { href: string; label: string };

/** The tabs themselves. `pathname` marks the current section; without it none is marked. */
export function NavTabs({ items, pathname }: { items: NavItem[]; pathname?: string }) {
  return (
    <nav aria-label="Main" className="flex flex-1 flex-wrap gap-x-7">
      {items.map(({ href, label }) => {
        const current =
          pathname !== undefined &&
          (href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`));
        return (
          <Link
            key={href}
            href={href}
            aria-current={current ? "page" : undefined}
            className={cn(
              "border-b-[3px] pt-[22px] pb-[19px] text-[15px] hover:border-ink",
              current ? "border-ink font-semibold" : "border-transparent font-medium",
            )}
          >
            {label}
          </Link>
        );
      })}
    </nav>
  );
}
