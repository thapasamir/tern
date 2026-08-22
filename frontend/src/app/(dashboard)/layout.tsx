import { Masthead } from "@/shared/ui/masthead";

import { navItems } from "./nav";

export default function DashboardLayout({ children }: LayoutProps<"/">) {
  return (
    <>
      <Masthead items={navItems} />
      <main className="mx-auto flex w-full max-w-[1280px] flex-col gap-10 px-5 pt-10 pb-20 sm:px-10">{children}</main>
    </>
  );
}
