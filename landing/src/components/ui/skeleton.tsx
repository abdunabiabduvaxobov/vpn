import type { HTMLAttributes } from "react";

import { cn } from "@/lib/utils";

/**
 * Skeleton — single-element loading placeholder. Used by /dashboard while the
 * Server Component awaits /api/me (Plan 06) and by /pay/success while polling
 * the invoice status (Plan 07). The base-ui native pattern is to compose
 * Skeleton from a styled div — no client directive needed since the
 * `animate-pulse` class is pure CSS.
 */
export function Skeleton({ className, ...rest }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      data-slot="skeleton"
      className={cn(
        "bg-surface-elevated/40 animate-pulse rounded-[var(--radius-md)]",
        className,
      )}
      {...rest}
    />
  );
}
