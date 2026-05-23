import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { AxiosError } from "axios";

import { replaceOffer, type PlanOffer } from "@/api/plans";
import type { LavaProduct } from "@/api/lava";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LavaOfferPicker } from "./LavaOfferPicker";

// ReplaceOfferDialog drives the PAY-15 price-versioning flow (ADR §19.13.4).
// Unlike a plain update, replaceOffer deactivates the OLD offer (grandfathering
// existing subscribers) and inserts a NEW active offer at the chosen price.
// periodicity + currency stay pinned to the old row's values (immutable per
// ADR §19.7.7), so the LavaOfferPicker filters its dropdown accordingly.
//
// The explicit-confirmation checkbox is required by ADR §19.13.4 — replacing
// a price is a one-way operation (no rollback) and the admin must
// affirmatively acknowledge the diff.
export interface ReplaceOfferDialogProps {
  planId: string;
  offer: PlanOffer | null; // null = dialog closed
  onClose: () => void;
}

export function ReplaceOfferDialog({
  planId,
  offer,
  onClose,
}: ReplaceOfferDialogProps) {
  const [newAmount, setNewAmount] = useState<string>("");
  const [newLavaOfferId, setNewLavaOfferId] = useState<string | null>(null);
  const [acknowledged, setAcknowledged] = useState(false);
  const qc = useQueryClient();

  const mutation = useMutation({
    mutationFn: () => {
      if (!offer) throw new Error("no offer");
      return replaceOffer(planId, offer.id, {
        amount: Number(newAmount),
        lava_offer_id: newLavaOfferId,
      });
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["admin", "plan", planId] });
      await qc.invalidateQueries({ queryKey: ["admin", "plans"] });
      toast.success(`Цена обновлена. Старые подписки сохраняют прежнюю.`);
      reset();
      onClose();
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(
        axiosErr.response?.data?.error ?? "Не удалось обновить цену",
      );
    },
  });

  function reset() {
    setNewAmount("");
    setNewLavaOfferId(null);
    setAcknowledged(false);
  }

  function onPick(row: LavaProduct) {
    setNewLavaOfferId(row.offerId);
    // Pre-fill the amount with the offer's lava-side price as a hint;
    // the admin can still override.
    setNewAmount(String(row.amount));
  }

  const amountNum = Number(newAmount);
  const valid = !!offer && !Number.isNaN(amountNum) && amountNum > 0 && acknowledged;

  return (
    <Dialog
      open={!!offer}
      onOpenChange={(next) => {
        if (!next) {
          reset();
          onClose();
        }
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Изменить цену оффера</DialogTitle>
          <DialogDescription>
            Старый оффер будет деактивирован, новый — активирован в одной
            транзакции. Существующие подписчики сохранят свою цену до
            истечения оплаченного периода (ADR §19.10 grandfathering).
            periodicity и currency не меняются.
          </DialogDescription>
        </DialogHeader>

        {offer && (
          <div className="space-y-4">
            <div className="rounded-md border border-border bg-muted/30 p-3 text-sm">
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <div className="text-xs uppercase text-muted-foreground">
                    Было
                  </div>
                  <div className="font-medium tabular-nums">
                    {offer.amount} {offer.currency}
                  </div>
                </div>
                <div>
                  <div className="text-xs uppercase text-muted-foreground">
                    Станет
                  </div>
                  <div className="font-medium tabular-nums">
                    {newAmount ? `${newAmount} ${offer.currency}` : "—"}
                  </div>
                </div>
              </div>
              <div className="mt-2 text-xs text-muted-foreground">
                {offer.periodicity}
              </div>
            </div>

            <div className="space-y-2">
              <Label>Новый оффер lava.top</Label>
              <LavaOfferPicker
                value={newLavaOfferId}
                onChange={onPick}
                filterCurrency={offer.currency}
                filterPeriodicity={offer.periodicity}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="replace-amount">Новая сумма (числом)</Label>
              <Input
                id="replace-amount"
                type="number"
                inputMode="decimal"
                step="0.01"
                value={newAmount}
                onChange={(e) => setNewAmount(e.target.value)}
                placeholder="Например 9.99"
              />
            </div>

            <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/5 p-3 text-sm">
              <Checkbox
                id="replace-ack"
                checked={acknowledged}
                onCheckedChange={(v) => setAcknowledged(v === true)}
              />
              <Label htmlFor="replace-ack" className="font-normal leading-snug">
                Я понимаю, что новая цена применяется только к новым
                подпискам, и операцию нельзя откатить через UI.
              </Label>
            </div>
          </div>
        )}

        <DialogFooter>
          <Button
            variant="outline"
            size="sm"
            type="button"
            onClick={() => {
              reset();
              onClose();
            }}
            disabled={mutation.isPending}
          >
            Отмена
          </Button>
          <Button
            variant="default"
            size="sm"
            type="button"
            disabled={!valid || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? "Сохраняем…" : "Заменить оффер"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
