import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Info } from "lucide-react";

import { listLavaProducts, type LavaProduct } from "@/api/lava";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

// LavaOfferPicker is the D-12 dropdown — admin selects a lava offer
// from the live /admin/lava/products catalogue. NO paste path: the
// component renders a Radix Select bound to the products query, and
// the value flows from the dropdown's onChange. The form NEVER reads
// from a free-text input.
//
// Tooltip warns about lava's hard floor ($5 USD / €5 EUR). Picking
// a sub-floor offer would cause lava to reject the checkout at runtime,
// so we surface it pre-emptively here.
export interface LavaOfferPickerProps {
  // The current value (lava offer UUID) or null when nothing picked yet.
  value: string | null;
  // Called when the admin selects a new row. The whole LavaProduct is
  // passed so the consumer can also derive periodicity / currency / amount
  // hints (the form keeps periodicity + currency as separate fields though).
  onChange: (row: LavaProduct) => void;
  // Optional filter by currency — when the consumer's parent form pins
  // the currency (e.g. editing an existing offer), filter the dropdown
  // to only rows in that currency so the admin can't accidentally pick
  // a USD row for a RUB offer.
  filterCurrency?: string;
  filterPeriodicity?: string;
  disabled?: boolean;
}

const LAVA_FLOOR: Record<string, number> = {
  USD: 5,
  EUR: 5,
  // RUB has no documented floor — leaving undefined skips the warning.
};

export function LavaOfferPicker({
  value,
  onChange,
  filterCurrency,
  filterPeriodicity,
  disabled,
}: LavaOfferPickerProps) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ["admin", "lava-products"],
    queryFn: listLavaProducts,
    staleTime: 60_000,
  });

  const rows = useMemo(() => {
    const all = data ?? [];
    return all.filter(
      (r) =>
        (!filterCurrency || r.currency === filterCurrency) &&
        (!filterPeriodicity || r.periodicity === filterPeriodicity),
    );
  }, [data, filterCurrency, filterPeriodicity]);

  const selected = rows.find((r) => r.offerId === value);
  const showFloorWarning =
    !!selected &&
    LAVA_FLOOR[selected.currency] !== undefined &&
    selected.amount < LAVA_FLOOR[selected.currency];

  return (
    <div className="space-y-2">
      <Select
        value={value ?? undefined}
        onValueChange={(next) => {
          // Defence in depth: only forward values we actually rendered.
          // A DOM-tampering admin could theoretically inject an arbitrary
          // value via DevTools, but the backend doesn't validate that the
          // UUID came from the dropdown anyway — see T-03-75 in plan body.
          const row = rows.find((r) => r.offerId === next);
          if (row) onChange(row);
        }}
        disabled={disabled || isLoading || isError}
      >
        <SelectTrigger>
          <SelectValue
            placeholder={
              isLoading
                ? "Загрузка каталога lava…"
                : isError
                  ? "Не удалось загрузить каталог"
                  : rows.length === 0
                    ? "Нет подходящих офферов"
                    : "Выберите оффер lava.top"
            }
          />
        </SelectTrigger>
        <SelectContent>
          {rows.map((r) => (
            <SelectItem key={`${r.offerId}-${r.currency}`} value={r.offerId}>
              <span className="font-medium">{r.productName || r.offerName}</span>
              <span className="ml-2 text-xs text-muted-foreground">
                {r.periodicity} · {r.amount} {r.currency}
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {showFloorWarning && selected && (
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <div
                className={cn(
                  "flex items-center gap-1.5 text-xs text-amber-500",
                )}
              >
                <Info className="size-3.5" />
                Сумма ниже минимума lava ({LAVA_FLOOR[selected.currency]}{" "}
                {selected.currency})
              </div>
            </TooltipTrigger>
            <TooltipContent>
              lava.top отказывает в создании счёта на сумму ниже{" "}
              {LAVA_FLOOR[selected.currency]} {selected.currency}. Выберите
              оффер с большей суммой или измените цену продукта в lava.
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      )}
    </div>
  );
}
