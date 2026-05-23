import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { MoreHorizontal, Plus } from "lucide-react";
import type { AxiosError } from "axios";

import {
  createOffer,
  deleteOffer,
  type PlanOffer,
} from "@/api/plans";
import type { LavaProduct } from "@/api/lava";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

import { LavaOfferPicker } from "./LavaOfferPicker";
import { ReplaceOfferDialog } from "./ReplaceOfferDialog";

// PERIODICITIES + CURRENCIES are the axes of the offer grid (ADR §19.13.3).
// The whitelist matches handler.allowedPeriodicities / allowedCurrencies in
// payment.go — the backend rejects any other combination with 400.
const PERIODICITIES = [
  "ONE_TIME",
  "MONTHLY",
  "PERIOD_90_DAYS",
  "PERIOD_180_DAYS",
  "PERIOD_YEAR",
] as const;
const CURRENCIES = ["USD", "EUR", "RUB"] as const;

const PERIODICITY_LABEL: Record<string, string> = {
  ONE_TIME: "Разовый",
  MONTHLY: "Месяц",
  PERIOD_90_DAYS: "90 дней",
  PERIOD_180_DAYS: "180 дней",
  PERIOD_YEAR: "Год",
};

export interface PlanOffersGridProps {
  planId: string;
  initial: PlanOffer[];
}

// PlanOffersGrid renders a (periodicity × currency) matrix per ADR §19.13.3.
// Each cell shows the active offer for that combo or a "+" affordance to
// create one. The kebab menu opens Edit (inline modal) / Replace
// (ReplaceOfferDialog — PAY-15) / Deactivate (deleteOffer soft-delete).
export function PlanOffersGrid({ planId, initial }: PlanOffersGridProps) {
  const qc = useQueryClient();
  const [createCell, setCreateCell] = useState<{
    periodicity: string;
    currency: string;
  } | null>(null);
  const [replaceTarget, setReplaceTarget] = useState<PlanOffer | null>(null);

  // We only render the latest ACTIVE offer per cell — historic (is_active=false)
  // offers are kept for grandfathering but the grid is meant to show the live
  // price catalogue.
  const byCell = new Map<string, PlanOffer>();
  for (const o of initial) {
    if (!o.is_active) continue;
    byCell.set(`${o.periodicity}|${o.currency}`, o);
  }

  const deleteMut = useMutation({
    mutationFn: (offerId: string) => deleteOffer(planId, offerId),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["admin", "plan", planId] });
      toast.success("Оффер деактивирован");
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(
        axiosErr.response?.data?.error ?? "Не удалось деактивировать",
      );
    },
  });

  return (
    <div className="space-y-4">
      <div className="overflow-x-auto rounded-md border border-border">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border bg-muted/30">
              <th className="px-3 py-2 text-left text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Период
              </th>
              {CURRENCIES.map((c) => (
                <th
                  key={c}
                  className="px-3 py-2 text-left text-xs font-medium uppercase tracking-wide text-muted-foreground"
                >
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {PERIODICITIES.map((p) => (
              <tr key={p} className="border-b border-border last:border-0">
                <td className="px-3 py-2 font-medium">{PERIODICITY_LABEL[p]}</td>
                {CURRENCIES.map((c) => {
                  const offer = byCell.get(`${p}|${c}`);
                  return (
                    <td
                      key={c}
                      className={cn(
                        "px-3 py-2 align-top",
                        offer && "bg-muted/10",
                      )}
                    >
                      {offer ? (
                        <OfferCell
                          offer={offer}
                          onReplace={() => setReplaceTarget(offer)}
                          onDeactivate={() => deleteMut.mutate(offer.id)}
                          deactivating={deleteMut.isPending}
                        />
                      ) : (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() =>
                            setCreateCell({ periodicity: p, currency: c })
                          }
                        >
                          <Plus className="size-3.5" />
                          Добавить
                        </Button>
                      )}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <CreateOfferDialog
        planId={planId}
        cell={createCell}
        onClose={() => setCreateCell(null)}
      />

      <ReplaceOfferDialog
        planId={planId}
        offer={replaceTarget}
        onClose={() => setReplaceTarget(null)}
      />
    </div>
  );
}

function OfferCell({
  offer,
  onReplace,
  onDeactivate,
  deactivating,
}: {
  offer: PlanOffer;
  onReplace: () => void;
  onDeactivate: () => void;
  deactivating: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-2">
      <div>
        <div className="font-medium tabular-nums">
          {offer.amount} {offer.currency}
        </div>
        <div className="font-mono text-[10px] text-muted-foreground">
          {offer.lava_offer_id ? (
            <span title={offer.lava_offer_id}>
              lava:{offer.lava_offer_id.slice(0, 8)}…
            </span>
          ) : (
            <span className="text-amber-500">offer_not_configured</span>
          )}
        </div>
      </div>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon">
            <MoreHorizontal className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={onReplace}>
            Изменить цену…
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onSelect={onDeactivate}
            disabled={deactivating}
            className="text-destructive focus:text-destructive"
          >
            Деактивировать
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

// CreateOfferDialog is the inline "+" cell modal. Periodicity + currency
// are pinned to the cell the admin clicked (immutable per ADR §19.7.7),
// so the LavaOfferPicker pre-filters its dropdown accordingly.
function CreateOfferDialog({
  planId,
  cell,
  onClose,
}: {
  planId: string;
  cell: { periodicity: string; currency: string } | null;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const [amount, setAmount] = useState("");
  const [lavaOfferId, setLavaOfferId] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: () => {
      if (!cell) throw new Error("no cell");
      return createOffer(planId, {
        periodicity: cell.periodicity,
        currency: cell.currency,
        amount: Number(amount),
        lava_offer_id: lavaOfferId,
      });
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["admin", "plan", planId] });
      toast.success("Оффер создан");
      reset();
      onClose();
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(
        axiosErr.response?.data?.error ?? "Не удалось создать оффер",
      );
    },
  });

  function reset() {
    setAmount("");
    setLavaOfferId(null);
  }

  function onPick(row: LavaProduct) {
    setLavaOfferId(row.offerId);
    setAmount(String(row.amount));
  }

  const amountNum = Number(amount);
  const valid = !!cell && !Number.isNaN(amountNum) && amountNum >= 0;

  return (
    <Dialog
      open={!!cell}
      onOpenChange={(next) => {
        if (!next) {
          reset();
          onClose();
        }
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            Новый оффер: {cell && PERIODICITY_LABEL[cell.periodicity]} ·{" "}
            {cell?.currency}
          </DialogTitle>
          <DialogDescription>
            periodicity и currency зафиксированы выбором ячейки (ADR §19.7.7).
            Если нужно создать оффер для другой комбинации — закройте и
            нажмите «+» в нужной клетке.
          </DialogDescription>
        </DialogHeader>

        {cell && (
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>Оффер lava.top (D-12 — только из списка)</Label>
              <LavaOfferPicker
                value={lavaOfferId}
                onChange={onPick}
                filterCurrency={cell.currency}
                filterPeriodicity={cell.periodicity}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="create-amount">Сумма (числом)</Label>
              <Input
                id="create-amount"
                type="number"
                inputMode="decimal"
                step="0.01"
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                placeholder="Например 9.99"
              />
            </div>

            <p className="text-xs text-muted-foreground">
              Оффер можно создать без lava_offer_id — он будет показываться
              на /pricing, но checkout отклонится с 409 offer_not_configured
              до тех пор, пока вы не привяжете lava-оффер.
            </p>
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
            {mutation.isPending ? "Создаём…" : "Создать"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
