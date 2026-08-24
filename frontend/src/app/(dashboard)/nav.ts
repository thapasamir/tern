import type { NavItem } from "@/shared/ui/nav-tabs";

/** The sections of the signed-in app. A section is added here when its screens exist. */
export const navItems: NavItem[] = [
  { href: "/", label: "Overview" },
  { href: "/endpoints", label: "Endpoints" },
];
