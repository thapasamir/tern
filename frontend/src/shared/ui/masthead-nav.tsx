"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { cn } from "@/shared/lib/utils";

export type NavItem = { href: string; label: string };

/** The tabs of the masthead. The current section is underlined and marked for screen readers. */
export function MastheadNav({ items }: { items: NavItem[] }) {
  const pathname = usePathname();

  return (
    <nav aria-label="Main" className="flex flex-1 flex-wrap gap-x-7">
      {items.map(({ href, label }) => {
        const current = href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`);
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
