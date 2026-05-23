import { Link } from "react-router-dom";

import type { AdminPlanSummary } from "@/api/plans";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { formatDate } from "@/lib/format";

import { DeletePlanDialog } from "./DeletePlanDialog";
import { PlanCodeBadge } from "./PlanCodeBadge";

// PlansTable renders the plans list per ADR §19.13.1. Columns: Code badge,
// Name, Status pill, Servers count, Devices (-1 → ∞), Active users, Updated,
// row actions (Edit + soft-delete).
//
// D-32 §4: system plans hide the delete affordance (UI mirrors backend 403).
// The DeletePlanDialog component is also defensive but the cleanest UX is
// to not even show it.
export function PlansTable({ plans }: { plans: AdminPlanSummary[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-[140px]">Код</TableHead>
          <TableHead>Название</TableHead>
          <TableHead className="w-[120px]">Статус</TableHead>
          <TableHead className="w-[100px] text-right">Серверы</TableHead>
          <TableHead className="w-[110px] text-right">Устройства</TableHead>
          <TableHead className="w-[160px] text-right">
            Активных пользователей
          </TableHead>
          <TableHead className="w-[180px]">Обновлён</TableHead>
          <TableHead className="w-[160px]" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {plans.length === 0 ? (
          <TableRow>
            <TableCell
              colSpan={8}
              className="text-center text-sm text-muted-foreground"
            >
              Пока нет тарифов. Создайте первый — он сразу появится
              в публичном /api/v1/plans.
            </TableCell>
          </TableRow>
        ) : (
          plans.map((p) => (
            <TableRow key={p.id} className={cn(!p.is_active && "opacity-60")}>
              <TableCell>
                <PlanCodeBadge code={p.code} />
              </TableCell>
              <TableCell className="font-medium">{p.name}</TableCell>
              <TableCell>
                {p.is_system ? (
                  <Badge variant="outline">Системный</Badge>
                ) : p.is_active ? (
                  <Badge>Активен</Badge>
                ) : (
                  <Badge variant="outline">Неактивен</Badge>
                )}
              </TableCell>
              <TableCell className="text-right tabular-nums">
                {p.server_count}
              </TableCell>
              <TableCell className="text-right tabular-nums">
                {p.max_devices === -1 ? "∞" : p.max_devices}
              </TableCell>
              <TableCell className="text-right tabular-nums">
                {p.active_user_count}
              </TableCell>
              <TableCell className="text-muted-foreground">
                {formatDate(p.updated_at)}
              </TableCell>
              <TableCell className="text-right">
                <div className="flex items-center justify-end gap-1">
                  <Button variant="ghost" size="sm" asChild>
                    <Link to={`/plans/${p.id}`}>Изменить</Link>
                  </Button>
                  {/* D-32 §4: system plans cannot be deleted — hide the
                      affordance entirely. The backend ALSO returns 403
                      even with ?force=true, so this is defence in depth. */}
                  {!p.is_system && <DeletePlanDialog plan={p} />}
                </div>
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  );
}
