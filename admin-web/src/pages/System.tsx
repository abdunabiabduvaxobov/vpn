import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AxiosError } from "axios";

import {
  createBroadcast,
  deleteBroadcast,
  getDepsHealth,
  listBroadcasts,
  listFeatureFlags,
  setFeatureFlag,
  updateBroadcast,
  type Broadcast,
  type BroadcastInput,
  type BroadcastSeverity,
  type FeatureFlag,
} from "@/api/system";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { formatDate } from "@/lib/format";

// maintenance_mode is the one flag that locks all non-admins out (503). The
// confirm copy warns the operator; the backend Maintenance middleware exempts
// admin routes so the panel keeps working (T-07-42).
const MAINTENANCE_FLAG = "maintenance_mode";

function statusTone(status: string): string {
  switch (status) {
    case "ok":
      return "bg-emerald-500/10 text-emerald-300 ring-emerald-500/30";
    case "down":
      return "bg-destructive/10 text-destructive ring-destructive/40";
    default:
      return "bg-muted text-muted-foreground ring-border";
  }
}

export function System() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Система</h1>
        <p className="text-sm text-muted-foreground">
          Здоровье зависимостей, фичефлаги, режим обслуживания и
          объявления-баннеры.
        </p>
      </div>

      <Tabs defaultValue="health">
        <TabsList>
          <TabsTrigger value="health">Зависимости</TabsTrigger>
          <TabsTrigger value="flags">Фичефлаги</TabsTrigger>
          <TabsTrigger value="broadcasts">Объявления</TabsTrigger>
        </TabsList>
        <TabsContent value="health" className="pt-4">
          <DepsHealthTab />
        </TabsContent>
        <TabsContent value="flags" className="pt-4">
          <FeatureFlagsTab />
        </TabsContent>
        <TabsContent value="broadcasts" className="pt-4">
          <BroadcastsTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}

// --- Deps-health tab ------------------------------------------------------

function DepsHealthTab() {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "deps-health"],
    queryFn: getDepsHealth,
    refetchInterval: 15_000,
  });

  if (isLoading) {
    return <Skeleton className="h-40 w-full" />;
  }
  if (isError || !data) {
    return (
      <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
        Не удалось загрузить состояние: {(error as Error)?.message}
      </div>
    );
  }

  const deps: { label: string; status: string }[] = [
    { label: "PostgreSQL", status: data.postgres },
    { label: "Redis", status: data.redis },
    { label: "lava.top", status: data.lava },
  ];

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-3">
        {deps.map((d) => (
          <Card key={d.label}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">
                {d.label}
              </CardTitle>
            </CardHeader>
            <CardContent>
              <span
                className={cn(
                  "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium uppercase ring-1 ring-inset",
                  statusTone(d.status),
                )}
              >
                {d.status}
              </span>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-base">Туннельные серверы</CardTitle>
        </CardHeader>
        <CardContent>
          {data.tunnel_servers.length === 0 ? (
            <div className="rounded-md border border-dashed border-border p-6 text-center text-sm text-muted-foreground">
              Туннельные серверы не зарегистрированы.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Хост</TableHead>
                  <TableHead className="w-[120px]">Свежесть</TableHead>
                  <TableHead className="w-[100px]">Нагрузка</TableHead>
                  <TableHead className="w-[200px]">Последний сигнал</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.tunnel_servers.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell>
                      <div className="font-medium">{s.hostname}</div>
                      {!s.is_active && (
                        <div className="text-xs text-muted-foreground">
                          неактивен
                        </div>
                      )}
                    </TableCell>
                    <TableCell>
                      <span
                        className={cn(
                          "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ring-1 ring-inset",
                          s.fresh
                            ? "bg-emerald-500/10 text-emerald-300 ring-emerald-500/30"
                            : "bg-destructive/10 text-destructive ring-destructive/40",
                        )}
                      >
                        {s.fresh ? "свежий" : "устарел"}
                      </span>
                    </TableCell>
                    <TableCell className="text-sm tabular-nums">
                      {s.current_load}%
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatDate(s.last_seen_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// --- Feature flags tab ----------------------------------------------------

function FeatureFlagsTab() {
  const qc = useQueryClient();
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "feature-flags"],
    queryFn: listFeatureFlags,
  });
  const [pendingMaintenance, setPendingMaintenance] =
    useState<{ flag: FeatureFlag; next: boolean } | null>(null);

  const mutation = useMutation({
    mutationFn: ({
      key,
      value,
      reason,
    }: {
      key: string;
      value: boolean;
      reason: string;
    }) => setFeatureFlag(key, value, reason),
    onSuccess: async (_data, vars) => {
      await qc.invalidateQueries({ queryKey: ["admin", "feature-flags"] });
      toast.success(`Флаг ${vars.key}: ${vars.value ? "включён" : "выключен"}`);
      setPendingMaintenance(null);
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(axiosErr.response?.data?.error ?? "Не удалось изменить флаг");
    },
  });

  function applyToggle(flag: FeatureFlag, next: boolean) {
    // The maintenance flag (and anything that gates non-admin access) gets an
    // extra confirm because flipping it ON returns 503 to every non-admin.
    if (flag.key === MAINTENANCE_FLAG && next) {
      setPendingMaintenance({ flag, next });
      return;
    }
    mutation.mutate({
      key: flag.key,
      value: next,
      reason: `toggle ${flag.key} -> ${next} via admin panel`,
    });
  }

  if (isLoading) {
    return <Skeleton className="h-40 w-full" />;
  }
  if (isError || !data) {
    return (
      <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
        Не удалось загрузить флаги: {(error as Error)?.message}
      </div>
    );
  }

  return (
    <Card>
      <CardContent className="p-0">
        {data.length === 0 ? (
          <div className="p-6 text-center text-sm text-muted-foreground">
            Фичефлаги не настроены.
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Флаг</TableHead>
                <TableHead className="w-[200px]">Изменён</TableHead>
                <TableHead className="w-[100px] text-right">
                  Состояние
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((flag) => (
                <TableRow key={flag.key}>
                  <TableCell>
                    <div className="font-mono text-sm">{flag.key}</div>
                    {flag.key === MAINTENANCE_FLAG && (
                      <div className="text-xs text-amber-300">
                        блокирует не-админов (503)
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {formatDate(flag.updated_at)}
                  </TableCell>
                  <TableCell className="text-right">
                    <Switch
                      checked={flag.value}
                      disabled={mutation.isPending}
                      onCheckedChange={(next) => applyToggle(flag, next)}
                      aria-label={`Переключить ${flag.key}`}
                    />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>

      <Dialog
        open={!!pendingMaintenance}
        onOpenChange={(open) => !open && setPendingMaintenance(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Включить режим обслуживания?</DialogTitle>
            <DialogDescription>
              Это вернёт 503 всем не-админам. Админ-панель продолжит работать —
              маршруты админки исключены из middleware обслуживания. Выключите
              флаг, чтобы вернуть доступ.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              type="button"
              onClick={() => setPendingMaintenance(null)}
              disabled={mutation.isPending}
            >
              Отмена
            </Button>
            <Button
              variant="destructive"
              size="sm"
              type="button"
              disabled={mutation.isPending}
              onClick={() =>
                pendingMaintenance &&
                mutation.mutate({
                  key: pendingMaintenance.flag.key,
                  value: true,
                  reason: "enable maintenance_mode via admin panel",
                })
              }
            >
              Включить обслуживание
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

// --- Broadcasts tab -------------------------------------------------------

const SEVERITIES: BroadcastSeverity[] = ["info", "warning", "critical"];

function BroadcastsTab() {
  const qc = useQueryClient();
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "broadcasts"],
    queryFn: listBroadcasts,
  });
  const [editing, setEditing] = useState<Broadcast | null>(null);
  const [creating, setCreating] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Broadcast | null>(null);

  async function invalidate() {
    await qc.invalidateQueries({ queryKey: ["admin", "broadcasts"] });
  }

  const createMutation = useMutation({
    mutationFn: (input: BroadcastInput) => createBroadcast(input),
    onSuccess: async () => {
      await invalidate();
      toast.success("Объявление создано");
      setCreating(false);
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(axiosErr.response?.data?.error ?? "Не удалось создать");
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, input }: { id: string; input: BroadcastInput }) =>
      updateBroadcast(id, input),
    onSuccess: async () => {
      await invalidate();
      toast.success("Объявление обновлено");
      setEditing(null);
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(axiosErr.response?.data?.error ?? "Не удалось обновить");
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteBroadcast(id),
    onSuccess: async () => {
      await invalidate();
      toast.success("Объявление удалено");
      setPendingDelete(null);
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(axiosErr.response?.data?.error ?? "Не удалось удалить");
    },
  });

  const busy =
    createMutation.isPending ||
    updateMutation.isPending ||
    deleteMutation.isPending;

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">Объявления</CardTitle>
        <Button size="sm" disabled={busy} onClick={() => setCreating(true)}>
          Создать
        </Button>
      </CardHeader>
      <CardContent className="p-0">
        {isLoading ? (
          <div className="p-4">
            <Skeleton className="h-32 w-full" />
          </div>
        ) : isError ? (
          <div className="m-4 rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
            Не удалось загрузить объявления: {(error as Error).message}
          </div>
        ) : (data ?? []).length === 0 ? (
          <div className="p-6 text-center text-sm text-muted-foreground">
            Объявлений пока нет.
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Заголовок</TableHead>
                <TableHead className="w-[120px]">Важность</TableHead>
                <TableHead className="w-[180px]">Окно показа</TableHead>
                <TableHead className="w-[120px]" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {(data ?? []).map((b) => (
                <TableRow key={b.id}>
                  <TableCell>
                    <div className="font-medium">{b.title}</div>
                    <div className="line-clamp-1 text-xs text-muted-foreground">
                      {b.body}
                    </div>
                  </TableCell>
                  <TableCell>
                    <span
                      className={cn(
                        "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ring-1 ring-inset",
                        b.severity === "critical"
                          ? "bg-destructive/10 text-destructive ring-destructive/40"
                          : b.severity === "warning"
                            ? "bg-amber-500/10 text-amber-300 ring-amber-500/30"
                            : "bg-sky-500/10 text-sky-300 ring-sky-500/30",
                      )}
                    >
                      {b.severity}
                    </span>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {b.starts_at ? formatDate(b.starts_at) : "сразу"} →{" "}
                    {b.ends_at ? formatDate(b.ends_at) : "∞"}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={busy}
                        onClick={() => setEditing(b)}
                      >
                        Изменить
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={busy}
                        className="text-destructive hover:text-destructive"
                        onClick={() => setPendingDelete(b)}
                      >
                        Удалить
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>

      <BroadcastFormDialog
        open={creating}
        onOpenChange={setCreating}
        busy={createMutation.isPending}
        onSubmit={(input) => createMutation.mutate(input)}
      />
      <BroadcastFormDialog
        open={!!editing}
        onOpenChange={(open) => !open && setEditing(null)}
        broadcast={editing ?? undefined}
        busy={updateMutation.isPending}
        onSubmit={(input) =>
          editing && updateMutation.mutate({ id: editing.id, input })
        }
      />

      <Dialog
        open={!!pendingDelete}
        onOpenChange={(open) => !open && setPendingDelete(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Удалить объявление?</DialogTitle>
            <DialogDescription>
              Объявление будет удалено и перестанет показываться в приложении и
              на лендинге.
            </DialogDescription>
          </DialogHeader>
          {pendingDelete && (
            <div className="rounded-md border border-border bg-muted/30 p-3 text-sm font-medium">
              {pendingDelete.title}
            </div>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              type="button"
              onClick={() => setPendingDelete(null)}
              disabled={busy}
            >
              Отмена
            </Button>
            <Button
              variant="destructive"
              size="sm"
              type="button"
              disabled={busy}
              onClick={() =>
                pendingDelete && deleteMutation.mutate(pendingDelete.id)
              }
            >
              Удалить
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

// BroadcastFormDialog handles both create and edit. starts_at/ends_at use a
// datetime-local input and are serialised to RFC3339 (UTC) on submit; empty
// means "no bound".
function BroadcastFormDialog({
  open,
  onOpenChange,
  broadcast,
  busy,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  broadcast?: Broadcast;
  busy: boolean;
  onSubmit: (input: BroadcastInput) => void;
}) {
  const [title, setTitle] = useState(broadcast?.title ?? "");
  const [body, setBody] = useState(broadcast?.body ?? "");
  const [severity, setSeverity] = useState<BroadcastSeverity>(
    broadcast?.severity ?? "info",
  );
  const [locale, setLocale] = useState(broadcast?.locale ?? "");
  const [targetTier, setTargetTier] = useState(broadcast?.target_tier ?? "");
  const [startsAt, setStartsAt] = useState(toLocalInput(broadcast?.starts_at));
  const [endsAt, setEndsAt] = useState(toLocalInput(broadcast?.ends_at));
  const [err, setErr] = useState<string | null>(null);

  // Reset the form whenever a different broadcast (or create) opens.
  const seedKey = broadcast?.id ?? "new";
  const [lastSeed, setLastSeed] = useState(seedKey);
  if (open && lastSeed !== seedKey) {
    setLastSeed(seedKey);
    setTitle(broadcast?.title ?? "");
    setBody(broadcast?.body ?? "");
    setSeverity(broadcast?.severity ?? "info");
    setLocale(broadcast?.locale ?? "");
    setTargetTier(broadcast?.target_tier ?? "");
    setStartsAt(toLocalInput(broadcast?.starts_at));
    setEndsAt(toLocalInput(broadcast?.ends_at));
    setErr(null);
  }

  function handleSubmit() {
    if (!title.trim() || !body.trim()) {
      setErr("Заголовок и текст обязательны");
      return;
    }
    setErr(null);
    onSubmit({
      title: title.trim(),
      body: body.trim(),
      severity,
      locale: locale.trim() || null,
      target_tier: targetTier.trim() || null,
      starts_at: fromLocalInput(startsAt),
      ends_at: fromLocalInput(endsAt),
    });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {broadcast ? "Изменить объявление" : "Новое объявление"}
          </DialogTitle>
          <DialogDescription>
            Объявления показываются на лендинге и в мобильном приложении в
            заданном окне времени.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-2">
            <Label htmlFor="b-title">Заголовок</Label>
            <Input
              id="b-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              disabled={busy}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="b-body">Текст</Label>
            <Textarea
              id="b-body"
              rows={3}
              value={body}
              onChange={(e) => setBody(e.target.value)}
              disabled={busy}
            />
          </div>
          <div className="space-y-2">
            <Label>Важность</Label>
            <Select
              value={severity}
              onValueChange={(v) => setSeverity(v as BroadcastSeverity)}
              disabled={busy}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SEVERITIES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {s}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-2">
              <Label htmlFor="b-locale">Локаль (необяз.)</Label>
              <Input
                id="b-locale"
                placeholder="ru / en / es"
                value={locale}
                onChange={(e) => setLocale(e.target.value)}
                disabled={busy}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="b-tier">Тариф (необяз.)</Label>
              <Input
                id="b-tier"
                placeholder="free / premium…"
                value={targetTier}
                onChange={(e) => setTargetTier(e.target.value)}
                disabled={busy}
              />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-2">
              <Label htmlFor="b-starts">Начало (необяз.)</Label>
              <Input
                id="b-starts"
                type="datetime-local"
                value={startsAt}
                onChange={(e) => setStartsAt(e.target.value)}
                disabled={busy}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="b-ends">Конец (необяз.)</Label>
              <Input
                id="b-ends"
                type="datetime-local"
                value={endsAt}
                onChange={(e) => setEndsAt(e.target.value)}
                disabled={busy}
              />
            </div>
          </div>
          {err && <div className="text-xs text-destructive">{err}</div>}
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
          <Button size="sm" type="button" onClick={handleSubmit} disabled={busy}>
            {broadcast ? "Сохранить" : "Создать"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// toLocalInput renders an RFC3339 timestamp as the YYYY-MM-DDTHH:mm value a
// datetime-local input expects (local time). fromLocalInput is the inverse —
// an empty string yields null (no bound), otherwise an ISO/UTC string.
function toLocalInput(iso: string | null | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}`;
}

function fromLocalInput(value: string): string | null {
  if (!value) return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return null;
  return d.toISOString();
}
