import { cva, type VariantProps } from "class-variance-authority";
import { Slot } from "radix-ui";
import * as React from "react";

import { cn } from "@/shared/lib/utils";

/**
 * Ink is the primary action. Bill is kept for the one destructive button on a screen. Every
 * target is 44px tall (36px for the small size, used inside dense rows).
 */
const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-sm border text-[15px] font-semibold whitespace-nowrap transition-colors disabled:pointer-events-none disabled:border-rule disabled:bg-transparent disabled:text-wing [&_svg]:pointer-events-none [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "border-ink bg-ink text-fog hover:border-graphite hover:bg-graphite",
        outline: "border-ink bg-transparent text-ink hover:bg-paper",
        quiet: "rounded-none border-0 border-b border-ink bg-transparent px-1 font-medium text-ink hover:bg-paper",
        destructive: "border-bill bg-transparent text-bill hover:bg-bill-tint",
      },
      size: {
        default: "h-11 px-5",
        sm: "h-9 px-4 text-sm",
        icon: "size-11",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

type ButtonProps = React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    /** Render the child element (a link, say) with the button's look instead of a <button>. */
    asChild?: boolean;
  };

function Button({ className, variant, size, asChild = false, ...props }: ButtonProps) {
  const Comp = asChild ? Slot.Root : "button";
  return (
    <Comp
      data-slot="button"
      className={cn(buttonVariants({ variant, size }), variant === "quiet" && "h-auto py-2", className)}
      {...props}
    />
  );
}

export { Button, buttonVariants };
