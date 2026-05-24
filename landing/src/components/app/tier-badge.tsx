import type { HTMLAttributes } from "react";

import { cn } from "@/lib/utils";

/**
 * TierBadge — small pill rendered next to the user's email on /dashboard and
 * inside /pricing CTA states. Server-component-friendly: accepts the label as
 * a prop instead of pulling next-intl directly, so the caller can choose
 * `t('dashboard.plan.pro')` from either a server or client tree.
 *
 * Tier mapping:
 *   - "pro"  → solid accent fill (the user has paid)
 *   - "free" → outlined muted (the user has not — drives "Get Pro" CTA copy)
 */

export type Tier = "free" | "pro";

interface Props extends HTMLAttributes<HTMLSpanElement> {
  tier: Tier;
  label: string;
}

export function TierBadge({ tier, label, className, ...rest }: Props) {
  return (
    <span
      data-slot="tier-badge"
      data-tier={tier}
      className={cn(
        "inline-flex items-center rounded-full px-2.5 py-0.5 font-mono text-xs font-medium uppercase tracking-wider",
        tier === "pro"
          ? "bg-accent text-accent-foreground"
          : "border border-border text-muted-foreground",
        className,
      )}
      {...rest}
    >
      {label}
    </span>
  );
}
