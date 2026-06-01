import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  Ban,
  Calendar,
  CheckCircle2,
  Copy,
  Unplug,
  XCircle,
} from "lucide-react";
import { toast } from "sonner";
import { AxiosError } from "axios";

import {
  cancelSubscription,
  disconnectUser,
  getUser,
  getUserAuditLog,
  getUserSessions,
  suspendUser,
  unsuspendUser,
  updateUser,
  type AdminUser,
  type UpdateUserInput,
} from "@/api/users";
import { Button } from "@/components/ui/button";
import { ConnectionsSection } from "@/components/ConnectionsSection";
import { DevicesSection } from "@/components/DevicesSection";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
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
import { formatDate, shortId } from "@/lib/format";
import { cn } from "@/lib/utils";
import {
  roleBadgeClass,
  tierBadgeClass,
  tierDeviceLimit,
  tierLabel,
  type Tier,
} from "@/lib/tier";

// toastUserError maps the per-user-control backend statuses to friendly
// toasts: 409 = already cancelled (force-cancel), 429 = force-disconnect
// throttle. Everything else falls back to the server {error} string.
function toastUserError(err: unknown, fallback: string) {
  const axiosErr = err as AxiosError<{ error?: string }>;
  const status = axiosErr.response?.status;
  if (status === 409) {
    toast.error("Подписка уже отменена");
    return;
  }
  if (status === 429) {
    toast.error("Слишком часто — подождите немного и повторите");
    return;
  }
  toast.error(axiosErr.response?.data?.error ?? fallback);
}

// Days-to-extend presets shown on the Upgrade action. 30/90/365 covers the
// three purchase tiers we document on the landing page; the custom dialog
// handles everything else.
const PRESET_DAYS: { label: string; days: number }[] = [
  { label: "+30 дней", days: 30 },
  { label: "+90 дней", days: 90 },
  { label: "+365 дней", days: 365 },
];

// Clamp extend_days client-side. Server has no cap (flagged in architect
// review) but an admin fat-fingering a year-count into the custom dialog
// would push the expiry out to the next century. 3650 = 10 years.
const MAX_EXTEND_DAYS = 3650;

export function UserDetail() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const qc = useQueryClient();

  const {
    data: user,
    isLoading,
    isError,
    error,
  } = useQuery({
    queryKey: ["admin", "user", id],
    queryFn: () => getUser(id),
    enabled: !!id,
  });

  const mutation = useMutation({
    mutationFn: (input: UpdateUserInput) => updateUser(id, input),
    onSuccess: async (_data, variables) => {
      // Re-fetch the detail view and invalidate the list so the updated
      // tier/expiration surfaces immediately in both places.
      await qc.invalidateQueries({ queryKey: ["admin", "user", id] });
      await qc.invalidateQueries({ queryKey: ["admin", "users"] });
      await qc.invalidateQueries({ queryKey: ["admin", "stats"] });
      toast.success(describeUpdate(variables));
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(axiosErr.response?.data?.error ?? "Не удалось обновить");
    },
  });

  const [customOpen, setCustomOpen] = useState(false);
  // Dialog state for the three per-user controls. Each opens a reason-carrying
  // confirm; force-cancel also carries a refund flag.
  const [suspendOpen, setSuspendOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [disconnectOpen, setDisconnectOpen] = useState(false);

  // invalidate re-reads the user + the per-user history queries after any
  // control mutation so the panel reflects the new state immediately.
  async function invalidateUser() {
    await qc.invalidateQueries({ queryKey: ["admin", "user", id] });
    await qc.invalidateQueries({ queryKey: ["admin", "users"] });
    await qc.invalidateQueries({ queryKey: ["admin", "user", id, "audit"] });
    await qc.invalidateQueries({ queryKey: ["admin", "user", id, "sessions"] });
  }

  const suspendMutation = useMutation({
    mutationFn: ({ reason, suspend }: { reason: string; suspend: boolean }) =>
      suspend ? suspendUser(id, reason) : unsuspendUser(id, reason),
    onSuccess: async (_data, vars) => {
      await invalidateUser();
      toast.success(vars.suspend ? "Пользователь заблокирован" : "Блокировка снята");
      setSuspendOpen(false);
    },
    onError: (err: unknown) => toastUserError(err, "Не удалось изменить блокировку"),
  });

  const cancelMutation = useMutation({
    mutationFn: ({ refund, reason }: { refund: boolean; reason: string }) =>
      cancelSubscription(id, { refund, reason }),
    onSuccess: async (res) => {
      await invalidateUser();
      await qc.invalidateQueries({ queryKey: ["admin", "stats"] });
      toast.success(
        res.refund_status === "intent_recorded"
          ? "Pro отменён, возврат зафиксирован"
          : "Pro отменён",
      );
      setCancelOpen(false);
    },
    onError: (err: unknown) => {
      toastUserError(err, "Не удалось отменить подписку");
      // 409 (already cancelled) is terminal for this dialog — close it so the
      // operator re-reads the now-refreshed state rather than retrying.
      if ((err as AxiosError).response?.status === 409) setCancelOpen(false);
    },
  });

  const disconnectMutation = useMutation({
    mutationFn: (reason: string) => disconnectUser(id, reason),
    onSuccess: async (res) => {
      await invalidateUser();
      await qc.invalidateQueries({ queryKey: ["admin", "user", id, "connections"] });
      toast.success(`Отключено подключений: ${res.killed_count}`);
      setDisconnectOpen(false);
    },
    onError: (err: unknown) => toastUserError(err, "Не удалось отключить"),
  });

  if (isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-1/3" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  if (isError || !user) {
    return (
      <div className="space-y-4">
        <Button variant="ghost" size="sm" onClick={() => navigate("/users")}>
          <ArrowLeft className="size-4" />
          К списку пользователей
        </Button>
        <Card className="border-destructive/40 bg-destructive/10">
          <CardContent className="p-4 text-sm text-destructive">
            Не удалось загрузить пользователя:{" "}
            {(error as Error)?.message ?? "не найден"}
          </CardContent>
        </Card>
      </div>
    );
  }

  async function copyId() {
    try {
      await navigator.clipboard.writeText(user!.id);
      toast.success("ID скопирован");
    } catch {
      toast.error("Не удалось скопировать");
    }
  }

  function applyUpgrade(tier: Exclude<Tier, "free">, days: number) {
    mutation.mutate({ subscription_tier: tier, extend_days: days });
  }

  function applyDowngrade() {
    // Downgrade to free also clears any existing expiration so the UI
    // doesn't show a stale "expires in 60 days" on a free account.
    mutation.mutate({ subscription_tier: "free", subscription_expires_at: "" });
  }

  const busy = mutation.isPending;
  const controlsBusy =
    suspendMutation.isPending ||
    cancelMutation.isPending ||
    disconnectMutation.isPending;

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate("/users")}
          className="-ml-2"
        >
          <ArrowLeft className="size-4" />
          К списку пользователей
        </Button>
      </div>

      {/* Profile card ----------------------------------------------------- */}
      <Card>
        <CardHeader>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-2">
              <CardTitle className="text-xl">
                {user.full_name || "Без имени"}
              </CardTitle>
              <div className="flex items-center gap-2 text-xs font-mono text-muted-foreground">
                <span>{user.id}</span>
                <button
                  type="button"
                  onClick={copyId}
                  className="rounded p-1 hover:bg-accent"
                  aria-label="Скопировать ID пользователя"
                >
                  <Copy className="size-3.5" />
                </button>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <span
                className={cn(
                  "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium",
                  tierBadgeClass(user.subscription_tier),
                )}
              >
                {tierLabel(user.subscription_tier)}
              </span>
              <span
                className={cn(
                  "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium capitalize",
                  roleBadgeClass(user.role),
                )}
              >
                {user.role}
              </span>
            </div>
          </div>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Stat
            label="Лимит устройств"
            value={`${tierDeviceLimit(user.subscription_tier)} шт.`}
          />
          <Stat
            label="Истекает"
            value={formatDate(user.subscription_expires_at)}
            highlight={!!user.subscription_expires_at}
          />
          <Stat label="Создан" value={formatDate(user.created_at)} />
          <Stat label="Роль" value={user.role} />
        </CardContent>
      </Card>

      {/* Telegram recovery binding (ADR-006). Rendered as a small info
          card so admins can see whether a user has a recovery channel
          and when it was established — useful when triaging "lost my
          phone" support requests. */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-base">Восстановление через Telegram</CardTitle>
        </CardHeader>
        <CardContent className="text-sm">
          {user.telegram_user_id ? (
            <div className="space-y-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="inline-flex items-center rounded-md bg-sky-500/10 px-2 py-0.5 text-xs font-medium text-sky-300 ring-1 ring-inset ring-sky-500/30">
                  Привязан
                </span>
                {/* Prefer @username when present (public, stable,
                    recognisable) over first_name. Fall back to just
                    the tg_id for pre-016 linked rows. */}
                {user.telegram_username ? (
                  <span className="text-sm">
                    @{user.telegram_username}
                  </span>
                ) : user.telegram_first_name ? (
                  <span className="text-sm">{user.telegram_first_name}</span>
                ) : null}
                <span className="font-mono text-xs text-muted-foreground">
                  tg_id {user.telegram_user_id}
                </span>
              </div>
              <div className="text-xs text-muted-foreground">
                с {formatDate(user.telegram_linked_at)}
              </div>
            </div>
          ) : (
            <div className="text-muted-foreground">
              Не привязан. Этот пользователь не сможет восстановить аккаунт
              при смене устройства.
            </div>
          )}
        </CardContent>
      </Card>

      {/* Action bar ------------------------------------------------------- */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Действия с подпиской</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-2">
          <TierActionMenu
            tier="pro"
            label="Активировать Pro"
            busy={busy}
            onSelect={(days) => applyUpgrade("pro", days)}
            currentTier={user.subscription_tier}
          />
          <Button
            variant="outline"
            size="sm"
            disabled={busy || user.subscription_tier === "free"}
            onClick={applyDowngrade}
          >
            <ArrowDown className="size-4" />
            Понизить до бесплатного
          </Button>
          <Separator orientation="vertical" className="mx-1 h-8" />
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => setCustomOpen(true)}
          >
            <Calendar className="size-4" />
            Своя дата окончания…
          </Button>
        </CardContent>
      </Card>

      {/* Per-user controls (ADMIN-02): suspend, force-cancel, force-disconnect.
          All three are reason-carrying; force-cancel + force-disconnect are
          destructive and confirm with the user id echoed. */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Контроль доступа</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={controlsBusy}
            onClick={() => setSuspendOpen(true)}
          >
            <Ban className="size-4" />
            Заблокировать / разблокировать…
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={controlsBusy}
            onClick={() => setCancelOpen(true)}
            className="text-destructive hover:text-destructive"
          >
            <XCircle className="size-4" />
            Принудительно отменить Pro…
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={controlsBusy}
            onClick={() => setDisconnectOpen(true)}
            className="text-destructive hover:text-destructive"
          >
            <Unplug className="size-4" />
            Принудительно отключить…
          </Button>
        </CardContent>
      </Card>

      {/* History tabs: audit log + active sessions + connections. */}
      <UserHistoryTabs userID={user.id} />

      <DevicesSection userID={user.id} />

      <CustomExpirationDialog
        open={customOpen}
        onOpenChange={setCustomOpen}
        user={user}
        busy={busy}
        onApply={(iso) => {
          mutation.mutate({
            // Always set the tier alongside the expiration so setting a
            // date on a free user implicitly upgrades them. Leaving tier
            // as-is would be surprising: "why does my custom date not
            // work for free users?"
            subscription_tier:
              user.subscription_tier === "free"
                ? "pro"
                : user.subscription_tier,
            subscription_expires_at: iso,
          });
          setCustomOpen(false);
        }}
      />

      <SuspendDialog
        open={suspendOpen}
        onOpenChange={setSuspendOpen}
        userID={user.id}
        busy={suspendMutation.isPending}
        onSubmit={(reason, suspend) =>
          suspendMutation.mutate({ reason, suspend })
        }
      />

      <CancelSubscriptionDialog
        open={cancelOpen}
        onOpenChange={setCancelOpen}
        userID={user.id}
        busy={cancelMutation.isPending}
        onSubmit={(refund, reason) => cancelMutation.mutate({ refund, reason })}
      />

      <DisconnectDialog
        open={disconnectOpen}
        onOpenChange={setDisconnectOpen}
        userID={user.id}
        busy={disconnectMutation.isPending}
        onSubmit={(reason) => disconnectMutation.mutate(reason)}
      />
    </div>
  );
}

// UserHistoryTabs renders the per-user audit log, active sessions, and the
// existing connections history in a tabbed view (ADMIN-02 history).
function UserHistoryTabs({ userID }: { userID: string }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">История</CardTitle>
      </CardHeader>
      <CardContent>
        <Tabs defaultValue="audit">
          <TabsList>
            <TabsTrigger value="audit">Аудит</TabsTrigger>
            <TabsTrigger value="sessions">Сессии</TabsTrigger>
            <TabsTrigger value="connections">Подключения</TabsTrigger>
          </TabsList>
          <TabsContent value="audit" className="pt-4">
            <AuditLogTab userID={userID} />
          </TabsContent>
          <TabsContent value="sessions" className="pt-4">
            <SessionsTab userID={userID} />
          </TabsContent>
          <TabsContent value="connections" className="pt-4">
            <ConnectionsSection userID={userID} />
          </TabsContent>
        </Tabs>
      </CardContent>
    </Card>
  );
}

function AuditLogTab({ userID }: { userID: string }) {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "user", userID, "audit"],
    queryFn: () => getUserAuditLog(userID, 1),
    enabled: !!userID,
  });

  if (isLoading) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    );
  }
  if (isError) {
    return (
      <div className="text-sm text-destructive">
        Не удалось загрузить аудит: {(error as Error).message}
      </div>
    );
  }
  const entries = data?.entries ?? [];
  if (entries.length === 0) {
    return (
      <div className="rounded-md border border-dashed border-border p-6 text-center text-sm text-muted-foreground">
        Записей аудита по этому пользователю пока нет.
      </div>
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-[180px]">Когда</TableHead>
          <TableHead className="w-[180px]">Действие</TableHead>
          <TableHead>Детали</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {entries.map((e) => (
          <TableRow key={e.id}>
            <TableCell className="text-xs text-muted-foreground">
              {formatDate(e.created_at)}
            </TableCell>
            <TableCell>
              <span className="rounded-md bg-muted px-2 py-0.5 text-xs font-medium font-mono ring-1 ring-inset ring-border">
                {e.action}
              </span>
            </TableCell>
            <TableCell className="text-xs text-muted-foreground">
              {e.details && Object.keys(e.details).length > 0 ? (
                <code className="break-all">{JSON.stringify(e.details)}</code>
              ) : (
                "—"
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function SessionsTab({ userID }: { userID: string }) {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "user", userID, "sessions"],
    queryFn: () => getUserSessions(userID),
    enabled: !!userID,
  });

  if (isLoading) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    );
  }
  if (isError) {
    return (
      <div className="text-sm text-destructive">
        Не удалось загрузить сессии: {(error as Error).message}
      </div>
    );
  }
  const sessions = data ?? [];
  if (sessions.length === 0) {
    return (
      <div className="rounded-md border border-dashed border-border p-6 text-center text-sm text-muted-foreground">
        Активных сессий нет.
      </div>
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-[120px]">ID</TableHead>
          <TableHead>Устройство</TableHead>
          <TableHead className="w-[180px]">Создана</TableHead>
          <TableHead className="w-[180px]">Истекает</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {sessions.map((s) => (
          <TableRow key={s.id}>
            <TableCell className="font-mono text-xs text-muted-foreground">
              {shortId(s.id)}
            </TableCell>
            <TableCell className="text-xs">{s.device_info || "—"}</TableCell>
            <TableCell className="text-xs text-muted-foreground">
              {formatDate(s.created_at)}
            </TableCell>
            <TableCell className="text-xs text-muted-foreground">
              {formatDate(s.expires_at)}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

// ReasonField is the shared required-reason textarea used by all three control
// dialogs. The backend 400s on an empty reason, so the submit button is
// disabled until at least one non-whitespace character is entered.
function ReasonField({
  value,
  onChange,
  disabled,
}: {
  value: string;
  onChange: (v: string) => void;
  disabled: boolean;
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor="reason">Причина (записывается в аудит)</Label>
      <Textarea
        id="reason"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        placeholder="Например: запрос поддержки #1234, нарушение ToS…"
        rows={3}
      />
    </div>
  );
}

function SuspendDialog({
  open,
  onOpenChange,
  userID,
  busy,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  userID: string;
  busy: boolean;
  onSubmit: (reason: string, suspend: boolean) => void;
}) {
  const [reason, setReason] = useState("");
  const trimmed = reason.trim();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Блокировка пользователя</DialogTitle>
          <DialogDescription>
            Блокировка немедленно отзывает все сессии и закрывает доступ к API
            (403 на следующем запросе). Разблокировка восстанавливает доступ.
            Причина обязательна и попадёт в журнал аудита.
          </DialogDescription>
        </DialogHeader>
        <div className="rounded-md border border-border bg-muted/30 p-2 font-mono text-xs text-muted-foreground">
          user_id: {userID}
        </div>
        <ReasonField value={reason} onChange={setReason} disabled={busy} />
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
            variant="outline"
            size="sm"
            type="button"
            disabled={busy || !trimmed}
            onClick={() => onSubmit(trimmed, false)}
          >
            Разблокировать
          </Button>
          <Button
            variant="destructive"
            size="sm"
            type="button"
            disabled={busy || !trimmed}
            onClick={() => onSubmit(trimmed, true)}
          >
            Заблокировать
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CancelSubscriptionDialog({
  open,
  onOpenChange,
  userID,
  busy,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  userID: string;
  busy: boolean;
  onSubmit: (refund: boolean, reason: string) => void;
}) {
  const [reason, setReason] = useState("");
  const [refund, setRefund] = useState(false);
  const trimmed = reason.trim();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Принудительная отмена Pro</DialogTitle>
          <DialogDescription>
            Сбрасывает пользователя на бесплатный план и помечает активный
            контракт отменённым. Возврат лишь ФИКСИРУЕТ намерение в аудите —
            никакой запрос на возврат в lava.top не отправляется. Если подписка
            уже отменена, вернётся 409.
          </DialogDescription>
        </DialogHeader>
        <div className="rounded-md border border-destructive/40 bg-destructive/10 p-2 font-mono text-xs">
          user_id: {userID}
        </div>
        <ReasonField value={reason} onChange={setReason} disabled={busy} />
        <div className="flex items-center gap-2">
          <Checkbox
            id="refund"
            checked={refund}
            onCheckedChange={(v) => setRefund(v === true)}
            disabled={busy}
          />
          <Label htmlFor="refund" className="text-sm font-normal">
            Зафиксировать намерение возврата
          </Label>
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
            variant="destructive"
            size="sm"
            type="button"
            disabled={busy || !trimmed}
            onClick={() => onSubmit(refund, trimmed)}
          >
            Отменить Pro
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DisconnectDialog({
  open,
  onOpenChange,
  userID,
  busy,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  userID: string;
  busy: boolean;
  onSubmit: (reason: string) => void;
}) {
  const [reason, setReason] = useState("");
  const trimmed = reason.trim();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Принудительно отключить пользователя</DialogTitle>
          <DialogDescription>
            Все живые подключения пользователя помечаются отключёнными; туннели
            разорвутся на ближайшем сборе устаревших соединений. Действие
            ограничено по частоте — повторный вызов в течение 30 секунд вернёт
            429.
          </DialogDescription>
        </DialogHeader>
        <div className="rounded-md border border-destructive/40 bg-destructive/10 p-2 font-mono text-xs">
          user_id: {userID}
        </div>
        <ReasonField value={reason} onChange={setReason} disabled={busy} />
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
            variant="destructive"
            size="sm"
            type="button"
            disabled={busy || !trimmed}
            onClick={() => onSubmit(trimmed)}
          >
            Отключить
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Stat({
  label,
  value,
  highlight,
}: {
  label: string;
  value: string;
  highlight?: boolean;
}) {
  return (
    <div>
      <div className="text-xs uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div
        className={cn(
          "mt-1 text-sm capitalize",
          highlight ? "font-medium text-foreground" : "",
        )}
      >
        {value}
      </div>
    </div>
  );
}

function TierActionMenu({
  tier,
  label,
  busy,
  currentTier,
  onSelect,
}: {
  tier: Exclude<Tier, "free">;
  label: string;
  busy: boolean;
  currentTier: Tier;
  onSelect: (days: number) => void;
}) {
  const isCurrent = currentTier === tier;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" disabled={busy}>
          {isCurrent ? (
            <CheckCircle2 className="size-4 text-emerald-400" />
          ) : (
            <ArrowUp className="size-4" />
          )}
          {label}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuLabel>
          {isCurrent
            ? `Продлить ${tierLabel(tier)}`
            : `Активировать ${tierLabel(tier)}`}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {PRESET_DAYS.map((p) => (
          <DropdownMenuItem key={p.days} onSelect={() => onSelect(p.days)}>
            {p.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function CustomExpirationDialog({
  open,
  onOpenChange,
  user,
  busy,
  onApply,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  user: AdminUser;
  busy: boolean;
  onApply: (isoOrEmpty: string) => void;
}) {
  // Seed the date picker with whatever the user currently has set, or 30
  // days from today if they have no expiration yet. Formatted as
  // YYYY-MM-DD (UTC) for <input type="date">.
  const defaultDate = (() => {
    if (user.subscription_expires_at) {
      return toDateInputValue(new Date(user.subscription_expires_at));
    }
    // setUTCDate mirrors toDateInputValue's UTC handling so the seed
    // date is "30 days from today in UTC", not "30 days from today in
    // whatever timezone the admin happens to be in".
    const d = new Date();
    d.setUTCDate(d.getUTCDate() + 30);
    return toDateInputValue(d);
  })();

  const [date, setDate] = useState(defaultDate);
  const [error, setError] = useState<string | null>(null);

  function handleApply() {
    if (!date) {
      setError("Выберите дату");
      return;
    }
    const parsed = new Date(`${date}T23:59:59.000Z`);
    if (Number.isNaN(parsed.getTime())) {
      setError("Некорректная дата");
      return;
    }
    const deltaDays = Math.ceil((parsed.getTime() - Date.now()) / 86_400_000);
    if (deltaDays > MAX_EXTEND_DAYS) {
      setError(`Максимальное продление — ${MAX_EXTEND_DAYS / 365} лет`);
      return;
    }
    setError(null);
    onApply(parsed.toISOString());
  }

  function handleClear() {
    // Empty string on subscription_expires_at clears the backend column.
    onApply("");
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Своя дата окончания</DialogTitle>
          <DialogDescription>
            Выберите точный день, когда подписка должна истечь (UTC).
            Если очистить дату, срок действия будет снят полностью.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          <Label htmlFor="expiration-date">Истекает</Label>
          <Input
            id="expiration-date"
            type="date"
            value={date}
            onChange={(e) => {
              setDate(e.target.value);
              setError(null);
            }}
            disabled={busy}
          />
          {error && (
            <div className="text-xs text-destructive">{error}</div>
          )}
        </div>
        <DialogFooter>
          <Button
            variant="ghost"
            size="sm"
            type="button"
            onClick={handleClear}
            disabled={busy}
          >
            Снять срок
          </Button>
          <div className="flex-1" />
          <Button
            variant="outline"
            size="sm"
            type="button"
            onClick={() => onOpenChange(false)}
            disabled={busy}
          >
            Отмена
          </Button>
          <Button size="sm" type="button" onClick={handleApply} disabled={busy}>
            Применить
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// toDateInputValue renders a Date as the YYYY-MM-DD string an
// <input type="date"> expects. Uses the UTC getters so the value
// round-trips cleanly through the dialog: the dialog label says
// "UTC", the picker reads UTC, the apply handler reconstructs the
// string as a UTC ISO timestamp, and an admin in a non-UTC timezone
// no longer sees their dates drift a day on every edit.
function toDateInputValue(d: Date): string {
  const yyyy = d.getUTCFullYear();
  const mm = String(d.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(d.getUTCDate()).padStart(2, "0");
  return `${yyyy}-${mm}-${dd}`;
}

// describeUpdate renders a human-readable toast message from the mutation
// input so the admin gets an unambiguous confirmation of what they just
// did ("Активирован Pro +90 дней"), not a generic "обновлено".
function describeUpdate(input: UpdateUserInput): string {
  if (input.subscription_tier === "free") {
    return "Понижен до бесплатного";
  }
  if (input.subscription_tier && input.extend_days) {
    return `Активирован ${tierLabel(input.subscription_tier)} +${input.extend_days} дней`;
  }
  if (input.subscription_expires_at === "") {
    return "Срок действия снят";
  }
  if (input.subscription_expires_at) {
    return `Дата окончания: ${new Date(input.subscription_expires_at).toLocaleDateString("ru-RU")}`;
  }
  return "Пользователь обновлён";
}
