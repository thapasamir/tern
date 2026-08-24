import Link from "next/link";

import { Button } from "@/shared/ui/button";

export default function DashboardNotFound() {
  return (
    <div className="flex max-w-xl flex-col items-start gap-3 rounded-sm border border-dashed border-wing px-6 py-7">
      <h1 className="text-[22px] font-semibold tracking-tight">Nothing here</h1>
      <p className="text-[15px] leading-relaxed text-graphite">That page doesn&apos;t exist, or it was deleted.</p>
      <Button asChild variant="outline">
        <Link href="/">Back to the overview</Link>
      </Button>
    </div>
  );
}
