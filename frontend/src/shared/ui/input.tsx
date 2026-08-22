import * as React from "react";

import { cn } from "@/shared/lib/utils";

/** A text field. An invalid one (aria-invalid) gets a heavier Bill border; say why next to it. */
function Input({ className, type = "text", ...props }: React.ComponentProps<"input">) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "h-11 w-full min-w-0 rounded-sm border border-ink bg-paper px-3.5 text-[15px] text-ink placeholder:text-wing disabled:cursor-not-allowed disabled:border-rule disabled:text-wing aria-invalid:border-2 aria-invalid:border-bill",
        className,
      )}
      {...props}
    />
  );
}

export { Input };
