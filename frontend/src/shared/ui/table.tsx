import * as React from "react";

import { cn } from "@/shared/lib/utils";

/**
 * A table in the logbook style: small caps headers over an ink rule, hairlines between rows, text
 * flush left. On a narrow screen it scrolls sideways inside its own box instead of squeezing.
 */
function Table({ className, ...props }: React.ComponentProps<"table">) {
  return (
    <div className="overflow-x-auto border-y border-ink">
      <table className={cn("w-full min-w-[640px] border-collapse text-left text-[15px]", className)} {...props} />
    </div>
  );
}

function Th({ className, ...props }: React.ComponentProps<"th">) {
  return <th scope="col" className={cn("eyebrow border-b border-ink px-4 py-3 font-normal", className)} {...props} />;
}

function Tr({ className, ...props }: React.ComponentProps<"tr">) {
  return <tr className={cn("border-b border-rule last:border-b-0 hover:bg-paper", className)} {...props} />;
}

function Td({ className, ...props }: React.ComponentProps<"td">) {
  return <td className={cn("px-4 py-3.5 align-middle", className)} {...props} />;
}

export { Table, Td, Th, Tr };
