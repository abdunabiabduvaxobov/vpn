import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { AxiosError } from "axios";

import { deletePlan, type AdminPlanSummary } from "@/api/plans";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// DeletePlanDialog gates the soft-delete behind a confirmation step.
// When the plan has active users (active_user_count > 0) the operator
// must type the plan code to confirm AND the backend call passes
// force=true to skip the 409 guard (ADR §19.13.4).
//
// Note: this component is only rendered when !plan.is_system — the
// table hides the trigger entirely for system plans (D-32 §4 — mirror
// of the backend's 403). Defence in depth: even if a determined user
// renders this component, the backend still returns 403.
export function DeletePlanDialog({ plan }: { plan: AdminPlanSummary }) {
  const [open, setOpen] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const qc = useQueryClient();

  const needsConfirmation = plan.active_user_count > 0;
  const canConfirm = needsConfirmation ? confirmation === plan.code : true;

  const mutation = useMutation({
    // force=true only when there are active users — otherwise the backend
    // already accepts the request without the flag.
    mutationFn: () => deletePlan(plan.id, needsConfirmation),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["admin", "plans"] });
      toast.success(`Тариф «${plan.name}» удалён`);
      setOpen(false);
      setConfirmation("");
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(
        axiosErr.response?.data?.error ?? "Не удалось удалить тариф",
      );
    },
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setConfirmation("");
      }}
    >
      <DialogTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="text-destructive hover:text-destructive"
        >
          Удалить
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Удалить тариф «{plan.name}»?</DialogTitle>
          <DialogDescription>
            {needsConfirmation
              ? `На этом тарифе ${plan.active_user_count} пользователей. После удаления они останутся на нём до окончания оплаченного периода, затем перейдут на системный тариф (cron expiry — ADR §19.10).`
              : "Тариф будет помечен как неактивный (is_active=false). Восстановить можно только через прямой SQL-доступ."}
          </DialogDescription>
        </DialogHeader>

        {needsConfirmation && (
          <div className="space-y-2">
            <Label htmlFor="confirm-code">
              Введите код тарифа для подтверждения:
            </Label>
            <Input
              id="confirm-code"
              value={confirmation}
              onChange={(e) => setConfirmation(e.target.value)}
              placeholder={plan.code}
              autoComplete="off"
              autoFocus
            />
          </div>
        )}

        <DialogFooter>
          <Button
            variant="outline"
            size="sm"
            type="button"
            onClick={() => setOpen(false)}
            disabled={mutation.isPending}
          >
            Отмена
          </Button>
          <Button
            variant="destructive"
            size="sm"
            type="button"
            disabled={!canConfirm || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? "Удаление…" : "Удалить"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
