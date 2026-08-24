import * as React from "react";

import { cn } from "@/shared/lib/utils";

/** A native select, dressed like Input. Native keeps it accessible and right on a phone. */
function Select({ className, ...props }: React.ComponentProps<"select">) {
  return (
    <select
      data-slot="select"
      className={cn(
        "h-11 w-full min-w-0 rounded-sm border border-ink bg-paper px-3 text-[15px] text-ink disabled:border-rule disabled:text-wing",
        className,
      )}
      {...props}
    />
  );
}

export { Select };
