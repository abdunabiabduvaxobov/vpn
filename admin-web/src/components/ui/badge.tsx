import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

// Minimal shadcn-style badge — we mostly pass our own className from
// tierBadgeClass/roleBadgeClass, so the variants here are a thin default
// set for the rare place we need a neutral badge.
const badgeVariants = cva(
  "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium",
  {
    variants: {
      variant: {
        default:
          "bg-primary text-primary-foreground ring-1 ring-inset ring-primary/20",
        secondary:
          "bg-secondary text-secondary-foreground ring-1 ring-inset ring-secondary/30",
        outline: "text-foreground ring-1 ring-inset ring-border",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />;
}

export { badgeVariants };
