// Tier and role color/label helpers used by the users table and detail view.
// Kept as plain functions (not a lookup map) so that TypeScript's union
// narrowing catches unknown values at compile time.

import type { AdminUser } from "@/api/users";

export type Tier = AdminUser["subscription_tier"];
export type Role = AdminUser["role"];

export const TIER_OPTIONS: Tier[] = ["free", "pro"];

// Tier labels mix Russian and English intentionally:
// "Pro" is the product brand name we keep in English for consistency
// with the mobile app and the lava.top product catalog. "Free" becomes
// "Бесплатный" in Russian because it's a common word, not a brand.
// (Migration 019 / D-08 collapsed the legacy premium+ultimate tiers into
// a single "pro" plan — the plans table only carries free + pro.)
export function tierLabel(tier: Tier): string {
  switch (tier) {
    case "free":
      return "Бесплатный";
    case "pro":
      return "Pro";
  }
}

// Returns Tailwind classes for a tier badge. Using ring-based borders
// instead of bg fills so tiers remain legible against both the card
// background and the table row hover state.
export function tierBadgeClass(tier: Tier): string {
  switch (tier) {
    case "free":
      return "bg-muted text-muted-foreground ring-1 ring-inset ring-border";
    case "pro":
      return "bg-sky-500/10 text-sky-300 ring-1 ring-inset ring-sky-500/30";
  }
}

export function roleBadgeClass(role: Role): string {
  switch (role) {
    case "admin":
      return "bg-rose-500/10 text-rose-300 ring-1 ring-inset ring-rose-500/30";
    case "user":
      return "bg-muted text-muted-foreground ring-1 ring-inset ring-border";
  }
}

// Device quota per tier — mirrors the plans catalog on the backend
// (migration 019 / D-08: free=1, pro=3). Used only for display hints; the
// actual enforcement is server-side.
export function tierDeviceLimit(tier: Tier): number {
  switch (tier) {
    case "free":
      return 1;
    case "pro":
      return 3;
  }
}
