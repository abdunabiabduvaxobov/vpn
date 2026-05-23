import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { ChevronLeft } from "lucide-react";

import { getPlan } from "@/api/plans";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PlanCodeBadge } from "@/components/plans/PlanCodeBadge";
import { PlanForm } from "@/components/plans/PlanForm";
import { PlanOffersGrid } from "@/components/plans/PlanOffersGrid";
import { PlanServersPicker } from "@/components/plans/PlanServersPicker";

// PlanDetail handles both:
//   /plans/new — Creation mode: renders PlanForm in create mode only
//                (no server/offer tabs — they need an id to attach to).
//   /plans/:id — Edit mode: three-tab layout (Limits / Servers / Pricing)
//                per ADR §19.13.2.
//
// The split mirrors the backend's two-step model — create the plan row
// first (POST /admin/plans), then wire servers / offers separately.
export function PlanDetail() {
  const { id } = useParams<{ id: string }>();
  const isNew = id === "new";

  const { data: plan, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "plan", id],
    queryFn: () => getPlan(id!),
    enabled: !isNew && !!id,
  });

  if (isNew) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" asChild>
            <Link to="/plans">
              <ChevronLeft className="size-4" />
              Назад
            </Link>
          </Button>
        </div>
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">Новый тариф</h1>
          <p className="text-sm text-muted-foreground">
            После создания вы сможете привязать серверы и цены на
            следующих вкладках.
          </p>
        </div>
        <PlanForm mode="create" />
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-9 w-80" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (isError || !plan) {
    return (
      <div className="space-y-4">
        <Button variant="ghost" size="sm" asChild>
          <Link to="/plans">
            <ChevronLeft className="size-4" />
            Назад к списку
          </Link>
        </Button>
        <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
          Не удалось загрузить тариф: {(error as Error)?.message ?? "не найден"}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" asChild>
          <Link to="/plans">
            <ChevronLeft className="size-4" />
            Назад
          </Link>
        </Button>
      </div>

      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">{plan.name}</h1>
        <PlanCodeBadge code={plan.code} />
      </div>

      <Tabs defaultValue="limits" className="space-y-4">
        <TabsList>
          <TabsTrigger value="limits">Лимиты</TabsTrigger>
          <TabsTrigger value="servers">
            Серверы ({plan.servers?.length ?? 0})
          </TabsTrigger>
          <TabsTrigger value="pricing">
            Цены ({(plan.offers ?? []).filter((o) => o.is_active).length})
          </TabsTrigger>
        </TabsList>

        <TabsContent value="limits">
          <PlanForm mode="edit" plan={plan} />
        </TabsContent>

        <TabsContent value="servers">
          <PlanServersPicker planId={plan.id} initial={plan.servers ?? []} />
        </TabsContent>

        <TabsContent value="pricing">
          <PlanOffersGrid planId={plan.id} initial={plan.offers ?? []} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
