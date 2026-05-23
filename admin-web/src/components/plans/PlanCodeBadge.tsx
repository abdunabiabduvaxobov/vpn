import { Badge } from "@/components/ui/badge";

// PlanCodeBadge renders the immutable plans.code slug. The font-mono
// styling signals to the operator that this is an opaque identifier
// (not a display name) — they shouldn't try to localise it. ADR §19.7.4
// pins code as immutable after plan creation.
export function PlanCodeBadge({ code }: { code: string }) {
  return (
    <Badge variant="secondary" className="font-mono">
      {code}
    </Badge>
  );
}
