import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Plus } from "lucide-react";

import { listPlans } from "@/api/plans";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { PlansTable } from "@/components/plans/PlansTable";

// Plans is the /plans index page (ADR §19.13.1). It lists ALL plans —
// active + inactive + system — and provides the entry point for both
// "new plan" creation and per-plan edit.
export function Plans() {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "plans"],
    queryFn: listPlans,
  });

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">Тарифы</h1>
          <p className="text-sm text-muted-foreground">
            Динамический каталог планов (ADR §19). Изменения публикуются
            мгновенно — публичный /api/v1/plans перестраивает кэш после
            каждой операции.
          </p>
        </div>
        <Button asChild>
          <Link to="/plans/new">
            <Plus className="size-4" />
            Новый тариф
          </Link>
        </Button>
      </div>

      <div className="rounded-lg border border-border">
        {isLoading ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : (
          <PlansTable plans={data ?? []} />
        )}
      </div>

      {isError && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
          Не удалось загрузить тарифы: {(error as Error).message}
        </div>
      )}
    </div>
  );
}
