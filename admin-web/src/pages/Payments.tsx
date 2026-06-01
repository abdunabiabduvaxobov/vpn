import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AxiosError } from "axios";

import {
  getWebhookEvent,
  listWebhookEvents,
  replayWebhookEvent,
  type WebhookEventDetail,
  type WebhookEventListItem,
} from "@/api/webhooks";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
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

// The status filter values match the backend lifecycle column. "all" clears
// the filter (the api client drops empty params).
const STATUS_FILTERS = [
  { value: "all", label: "Все статусы" },
  { value: "PENDING", label: "PENDING" },
  { value: "DELIVERED", label: "DELIVERED" },
  { value: "FAILED", label: "FAILED" },
  { value: "REPLAYED", label: "REPLAYED" },
];

function statusTone(status: string): string {
  switch (status) {
    case "DELIVERED":
      return "bg-emerald-500/10 text-emerald-300 ring-emerald-500/30";
    case "FAILED":
      return "bg-destructive/10 text-destructive ring-destructive/40";
    case "REPLAYED":
      return "bg-sky-500/10 text-sky-300 ring-sky-500/30";
    default:
      return "bg-muted text-muted-foreground ring-border";
  }
}

// Only DELIVERED or FAILED events are replayable (T-07-36).
function isReplayable(status: string): boolean {
  return status === "DELIVERED" || status === "FAILED";
}

export function Payments() {
  const qc = useQueryClient();
  const [status, setStatus] = useState("all");
  const [detailId, setDetailId] = useState<string | null>(null);
  const [pendingReplay, setPendingReplay] =
    useState<WebhookEventListItem | null>(null);

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "webhook-events", status],
    queryFn: () =>
      listWebhookEvents({
        status: status === "all" ? undefined : status,
        page: 1,
        limit: 50,
      }),
  });

  const replayMutation = useMutation({
    mutationFn: ({ id, reason }: { id: string; reason: string }) =>
      replayWebhookEvent(id, reason),
    onSuccess: async (res) => {
      // Invalidate the whole webhook-events family so every status filter
      // re-reads (the row may now have moved into the REPLAYED filter).
      await qc.invalidateQueries({ queryKey: ["admin", "webhook-events"] });
      toast.success(
        `Событие переотправлено (${res.outcome}), повторов: ${res.retried_count}`,
      );
      setPendingReplay(null);
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(
        axiosErr.response?.data?.error ?? "Не удалось переотправить событие",
      );
    },
  });

  const events = data?.events ?? [];

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Платежи</h1>
          <p className="text-sm text-muted-foreground">
            Журнал вебхуков lava.top. Email-адреса покупателей скрыты в списке;
            полный payload доступен по клику (доступ аудируется).
          </p>
        </div>
        <div className="w-[200px]">
          <Label className="text-xs text-muted-foreground">Статус</Label>
          <Select value={status} onValueChange={setStatus}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {STATUS_FILTERS.map((f) => (
                <SelectItem key={f.value} value={f.value}>
                  {f.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="p-4">
              <Skeleton className="h-40 w-full" />
            </div>
          ) : isError ? (
            <div className="m-4 rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
              Не удалось загрузить события: {(error as Error).message}
            </div>
          ) : events.length === 0 ? (
            <div className="p-6 text-center text-sm text-muted-foreground">
              Событий нет.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Тип события</TableHead>
                  <TableHead className="w-[120px]">Статус</TableHead>
                  <TableHead className="w-[100px]">Повторы</TableHead>
                  <TableHead className="w-[180px]">Получено</TableHead>
                  <TableHead className="w-[140px]" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {events.map((ev) => (
                  <TableRow
                    key={ev.id}
                    className="cursor-pointer"
                    onClick={() => setDetailId(ev.id)}
                  >
                    <TableCell>
                      <div className="font-mono text-sm">{ev.event_type}</div>
                      {ev.error && (
                        <div className="line-clamp-1 text-xs text-destructive">
                          {ev.error}
                        </div>
                      )}
                    </TableCell>
                    <TableCell>
                      <span
                        className={cn(
                          "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ring-1 ring-inset",
                          statusTone(ev.status),
                        )}
                      >
                        {ev.status}
                      </span>
                    </TableCell>
                    <TableCell className="text-sm tabular-nums text-muted-foreground">
                      {ev.retried_count}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatDate(ev.received_at)}
                    </TableCell>
                    <TableCell
                      className="text-right"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {isReplayable(ev.status) && (
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={replayMutation.isPending}
                          onClick={() => setPendingReplay(ev)}
                        >
                          Повторить
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <PayloadDialog
        id={detailId}
        onOpenChange={(open) => !open && setDetailId(null)}
      />

      <ReplayDialog
        event={pendingReplay}
        busy={replayMutation.isPending}
        onOpenChange={(open) => !open && setPendingReplay(null)}
        onSubmit={(reason) =>
          pendingReplay &&
          replayMutation.mutate({ id: pendingReplay.id, reason })
        }
      />
    </div>
  );
}

// PayloadDialog lazily fetches the full (unredacted) event by id when opened.
function PayloadDialog({
  id,
  onOpenChange,
}: {
  id: string | null;
  onOpenChange: (open: boolean) => void;
}) {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "webhook-event", id],
    queryFn: () => getWebhookEvent(id as string),
    enabled: !!id,
  });

  return (
    <Dialog open={!!id} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Payload вебхука</DialogTitle>
          <DialogDescription>
            Полное (нередактированное) тело события. Доступ к этому виду
            аудируется.
          </DialogDescription>
        </DialogHeader>
        {isLoading ? (
          <Skeleton className="h-40 w-full" />
        ) : isError ? (
          <div className="text-sm text-destructive">
            Не удалось загрузить событие: {(error as Error).message}
          </div>
        ) : data ? (
          <PayloadBody detail={data} />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function PayloadBody({ detail }: { detail: WebhookEventDetail }) {
  return (
    <div className="space-y-3 text-sm">
      <dl className="grid grid-cols-2 gap-2">
        <Meta label="ID" value={detail.id} mono />
        <Meta label="Тип" value={detail.event_type} mono />
        <Meta label="Статус" value={detail.status} />
        <Meta label="Повторы" value={String(detail.retried_count)} />
        <Meta label="Contract" value={detail.contract_id ?? "—"} mono />
        <Meta label="Invoice" value={detail.invoice_id ?? "—"} mono />
        <Meta label="Получено" value={formatDate(detail.received_at)} />
        <Meta label="Обработано" value={formatDate(detail.processed_at)} />
      </dl>
      {detail.error && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 p-2 text-xs text-destructive">
          {detail.error}
        </div>
      )}
      <pre className="max-h-[40vh] overflow-auto rounded-md border border-border bg-muted/30 p-3 text-xs">
        {JSON.stringify(detail.payload, null, 2)}
      </pre>
    </div>
  );
}

function Meta({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd className={cn("break-all text-sm", mono && "font-mono text-xs")}>
        {value}
      </dd>
    </div>
  );
}

function ReplayDialog({
  event,
  busy,
  onOpenChange,
  onSubmit,
}: {
  event: WebhookEventListItem | null;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (reason: string) => void;
}) {
  const [reason, setReason] = useState("");
  const trimmed = reason.trim();

  // Reset the reason whenever a different event opens the dialog.
  const [lastId, setLastId] = useState<string | null>(null);
  if (event && event.id !== lastId) {
    setLastId(event.id);
    setReason("");
  }

  return (
    <Dialog open={!!event} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Повторить вебхук?</DialogTitle>
          <DialogDescription>
            Событие будет повторно применено идемпотентно — тот же тариф, без
            повторного начисления. Статус станет REPLAYED, счётчик повторов
            увеличится. Причина обязательна и попадёт в аудит.
          </DialogDescription>
        </DialogHeader>
        {event && (
          <div className="rounded-md border border-border bg-muted/30 p-2 text-xs">
            <div className="font-mono">{event.event_type}</div>
            <div className="font-mono text-muted-foreground">{event.id}</div>
          </div>
        )}
        <div className="space-y-2">
          <Label htmlFor="replay-reason">Причина</Label>
          <Textarea
            id="replay-reason"
            rows={3}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            disabled={busy}
            placeholder="Например: пропущенное начисление, тикет #1234…"
          />
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            size="sm"
            type="button"
            onClick={() => onOpenChange(false)}
            disabled={busy}
          >
            Отмена
          </Button>
          <Button
            size="sm"
            type="button"
            disabled={busy || !trimmed}
            onClick={() => onSubmit(trimmed)}
          >
            Повторить
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
