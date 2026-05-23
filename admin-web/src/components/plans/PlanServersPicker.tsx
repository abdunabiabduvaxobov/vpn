import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle } from "lucide-react";
import type { AxiosError } from "axios";

import { listServers, type AdminServer } from "@/api/servers";
import { replacePlanServers } from "@/api/plans";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

// PlanServersPicker drives ADR §19.13.2 Tab 2 — a checkbox grid grouped
// by country. Atomic replace via PUT /admin/plans/:id/servers
// (replacePlanServers). The picker computes a diff between the persisted
// selection and the working selection so the operator sees what will be
// added / removed before saving.
//
// Removing a server that was previously selected fires the warning banner
// from ADR §19.13.2 — existing users on this plan won't be force-
// disconnected (D-23) but will rotate off the removed server on next
// connection.
export interface PlanServersPickerProps {
  planId: string;
  initial: AdminServer[];
}

export function PlanServersPicker({ planId, initial }: PlanServersPickerProps) {
  const qc = useQueryClient();

  // Working selection — keyed by server id. Initialised from the persisted
  // plan_servers join.
  const [selected, setSelected] = useState<Set<string>>(
    () => new Set(initial.map((s) => s.id)),
  );

  // Re-sync when the parent re-fetches (e.g. after a successful save in
  // a sibling tab or another agent run).
  useEffect(() => {
    setSelected(new Set(initial.map((s) => s.id)));
  }, [initial]);

  const { data: allServers, isLoading } = useQuery({
    queryKey: ["admin", "servers"],
    queryFn: listServers,
  });

  // Group by country. We only show active servers — inactive ones are
  // hidden because the backend rejects them with 422 anyway (T-03-65).
  const grouped = useMemo(() => {
    const active = (allServers ?? []).filter((s) => s.is_active);
    const m = new Map<string, AdminServer[]>();
    for (const s of active) {
      const k = s.country || "—";
      if (!m.has(k)) m.set(k, []);
      m.get(k)!.push(s);
    }
    return Array.from(m.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [allServers]);

  const initialIds = useMemo(() => new Set(initial.map((s) => s.id)), [initial]);
  const willAdd = useMemo(
    () => [...selected].filter((id) => !initialIds.has(id)),
    [selected, initialIds],
  );
  const willRemove = useMemo(
    () => [...initialIds].filter((id) => !selected.has(id)),
    [initialIds, selected],
  );
  const dirty = willAdd.length > 0 || willRemove.length > 0;

  const mutation = useMutation({
    mutationFn: () => replacePlanServers(planId, [...selected]),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["admin", "plan", planId] });
      await qc.invalidateQueries({ queryKey: ["admin", "plans"] });
      toast.success("Список серверов плана обновлён");
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(
        axiosErr.response?.data?.error ?? "Не удалось сохранить серверы",
      );
    },
  });

  function toggle(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  if (isLoading) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {willRemove.length > 0 && (
        <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/5 p-3 text-sm text-amber-600">
          <AlertTriangle className="size-4 shrink-0" />
          <div>
            Будет удалено серверов: {willRemove.length}. Существующие
            пользователи плана НЕ отключатся принудительно (D-23), но при
            следующей сессии получат другой сервер из набора плана.
          </div>
        </div>
      )}

      <div className="space-y-4">
        {grouped.map(([country, servers]) => (
          <div key={country} className="rounded-md border border-border p-3">
            <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              {country}
            </div>
            <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
              {servers.map((s) => {
                const id = `srv-${s.id}`;
                return (
                  <div
                    key={s.id}
                    className={cn(
                      "flex items-start gap-2 rounded-md border border-border bg-muted/20 p-2",
                      selected.has(s.id) && "border-primary/40 bg-primary/5",
                    )}
                  >
                    <Checkbox
                      id={id}
                      checked={selected.has(s.id)}
                      onCheckedChange={() => toggle(s.id)}
                    />
                    <Label
                      htmlFor={id}
                      className="cursor-pointer font-normal leading-tight"
                    >
                      <div className="text-sm font-medium">{s.hostname}</div>
                      <div className="font-mono text-xs text-muted-foreground">
                        {s.city || "—"} · {s.protocol}
                      </div>
                    </Label>
                  </div>
                );
              })}
            </div>
          </div>
        ))}
        {grouped.length === 0 && (
          <div className="rounded-md border border-border p-4 text-center text-sm text-muted-foreground">
            Нет активных серверов. Добавьте серверы на странице /servers.
          </div>
        )}
      </div>

      <div className="flex items-center justify-between border-t border-border pt-4">
        <div className="text-sm text-muted-foreground">
          Выбрано: <span className="tabular-nums">{selected.size}</span>
          {dirty && (
            <span className="ml-2 text-amber-500">
              (+{willAdd.length} / −{willRemove.length})
            </span>
          )}
        </div>
        <Button
          type="button"
          disabled={!dirty || mutation.isPending}
          onClick={() => mutation.mutate()}
        >
          {mutation.isPending ? "Сохраняем…" : "Сохранить серверы"}
        </Button>
      </div>
    </div>
  );
}
