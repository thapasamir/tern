import * as React from "react";

import { cn } from "@/shared/lib/utils";

/** A multi-line text field, dressed like Input. */
function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        "min-h-24 w-full min-w-0 rounded-sm border border-ink bg-paper px-3.5 py-3 text-[15px] text-ink placeholder:text-wing disabled:border-rule disabled:text-wing aria-invalid:border-2 aria-invalid:border-bill",
        className,
      )}
      {...props}
    />
  );
}

export { Textarea };
