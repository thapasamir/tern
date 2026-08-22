import * as React from "react";

import { cn } from "@/shared/lib/utils";

/** A field label: small caps in the mono. */
function Label({ className, ...props }: React.ComponentProps<"label">) {
  return <label data-slot="label" className={cn("eyebrow text-ink", className)} {...props} />;
}

export { Label };
