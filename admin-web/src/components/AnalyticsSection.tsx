import { useQuery } from "@tanstack/react-query";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import {
  Activity,
  Server as ServerIcon,
  Smartphone,
  Wallet,
} from "lucide-react";

import {
  getAnalytics,
  type BytesBucket,
  type PlatformCount,
  type TierCount,
  type TopServer,
} from "@/api/analytics";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { formatBytes, formatNumber } from "@/lib/format";
import { tierBadgeClass, tierLabel, type Tier } from "@/lib/tier";

// AnalyticsSection is the "deep-dive" half of the dashboard. It makes
// one round-trip to GET /admin/analytics and renders four widgets from
// the response. Each widget has its own empty/error state so a failure
// in one sub-query doesn't blank the whole section.
export function AnalyticsSection() {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "analytics", 30],
    queryFn: () => getAnalytics(30),
    refetchInterval: 5 * 60_000,
  });

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-[280px] w-full" />
        <div className="grid gap-4 md:grid-cols-3">
          <Skeleton className="h-[220px] w-full" />
          <Skeleton className="h-[220px] w-full" />
          <Skeleton className="h-[220px] w-full" />
        </div>
      </div>
    );
  }

  if (isError || !data) {
    return (
      <Card className="border-destructive/40 bg-destructive/10">
        <CardContent className="p-4 text-sm text-destructive">
          Не удалось загрузить аналитику:{" "}
          {(error as Error)?.message ?? "неизвестная ошибка"}
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <TrafficChart data={data.traffic} err={data.errors?.traffic} />
      <div className="grid gap-4 md:grid-cols-3">
        <PlatformCard data={data.platforms} err={data.errors?.platforms} />
        <TierCard data={data.tiers} err={data.errors?.tiers} />
        <TopServersCard
          data={data.top_servers}
          err={data.errors?.top_servers}
        />
      </div>
    </div>
  );
}

// --- Traffic chart --------------------------------------------------------

function TrafficChart({
  data,
  err,
}: {
  data: BytesBucket[] | null | undefined;
  err: string | undefined;
}) {
  // Convert bytes to megabytes for the Y axis so the numbers are
  // human-readable. The tooltip renders the raw value via formatBytes
  // so we keep full precision where it matters.
  const chartData = (data ?? []).map((row) => ({
    date: row.date,
    up: row.bytes_up,
    down: row.bytes_down,
  }));

  const totalUp = (data ?? []).reduce((sum, r) => sum + r.bytes_up, 0);
  const totalDown = (data ?? []).reduce((sum, r) => sum + r.bytes_down, 0);

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0">
        <div className="flex items-center gap-2">
          <Activity className="size-4 text-muted-foreground" />
          <CardTitle className="text-base">Трафик за 30 дней</CardTitle>
        </div>
        <div className="text-right text-xs text-muted-foreground">
          <div>↑ всего {formatBytes(totalUp)}</div>
          <div>↓ всего {formatBytes(totalDown)}</div>
        </div>
      </CardHeader>
      <CardContent>
        {err ? (
          <div className="flex h-[240px] items-center justify-center text-sm text-destructive">
            Ошибка: {err}
          </div>
        ) : chartData.length === 0 ? (
          <EmptyHint text="Нет данных о трафике за последние 30 дней." />
        ) : (
          <div className="h-[240px] w-full">
            <ResponsiveContainer>
              <AreaChart
                data={chartData}
                margin={{ top: 10, right: 12, left: 0, bottom: 0 }}
              >
                <defs>
                  <linearGradient id="tr-up" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#34d399" stopOpacity={0.5} />
                    <stop offset="95%" stopColor="#34d399" stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="tr-down" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#f472b6" stopOpacity={0.5} />
                    <stop offset="95%" stopColor="#f472b6" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid
                  strokeDasharray="3 3"
                  stroke="currentColor"
                  className="text-border/40"
                />
                <XAxis
                  dataKey="date"
                  tickFormatter={(v: string) => v.slice(5)}
                  stroke="currentColor"
                  className="text-xs text-muted-foreground"
                />
                <YAxis
                  stroke="currentColor"
                  className="text-xs text-muted-foreground"
                  width={56}
                  tickFormatter={(v: number) => formatBytes(v)}
                />
                <Tooltip
                  contentStyle={{
                    background: "hsl(240 6% 10%)",
                    border: "1px solid hsl(240 4% 22%)",
                    borderRadius: 8,
                    fontSize: 12,
                  }}
                  labelStyle={{ color: "#cbd5e1" }}
                  formatter={(value, name) => {
                    const v = typeof value === "number" ? value : 0;
                    const label =
                      name === "Исходящий" || name === "up"
                        ? "Исходящий"
                        : "Входящий";
                    return [formatBytes(v), label];
                  }}
                />
                <Area
                  type="monotone"
                  dataKey="up"
                  name="Исходящий"
                  stroke="#34d399"
                  fill="url(#tr-up)"
                  strokeWidth={2}
                />
                <Area
                  type="monotone"
                  dataKey="down"
                  name="Входящий"
                  stroke="#f472b6"
                  fill="url(#tr-down)"
                  strokeWidth={2}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        )}
        <div className="mt-3 flex items-center gap-4 text-xs text-muted-foreground">
          <LegendDot color="#34d399" label="Исходящий" />
          <LegendDot color="#f472b6" label="Входящий" />
        </div>
      </CardContent>
    </Card>
  );
}

// --- Platform breakdown ---------------------------------------------------

function PlatformCard({
  data,
  err,
}: {
  data: PlatformCount[] | null | undefined;
  err: string | undefined;
}) {
  const rows = data ?? [];
  const total = rows.reduce((s, r) => s + r.count, 0);

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <Smartphone className="size-4 text-muted-foreground" />
          <CardTitle className="text-base">Платформы</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {err ? (
          <div className="text-sm text-destructive">Ошибка: {err}</div>
        ) : total === 0 ? (
          <EmptyHint text="Устройств пока нет." />
        ) : (
          rows.map((row) => (
            <BarRow
              key={row.platform}
              label={labelForPlatform(row.platform)}
              count={row.count}
              total={total}
              color={colorForPlatform(row.platform)}
            />
          ))
        )}
      </CardContent>
    </Card>
  );
}

function labelForPlatform(platform: string): string {
  switch (platform) {
    case "android":
      return "Android";
    case "ios":
      return "iOS";
    case "unknown":
      return "Неизвестно";
    default:
      // Capitalise-first any unexpected values so the UI stays tidy.
      return platform.charAt(0).toUpperCase() + platform.slice(1);
  }
}

function colorForPlatform(platform: string): string {
  switch (platform) {
    case "android":
      return "#34d399"; // emerald
    case "ios":
      return "#60a5fa"; // sky
    default:
      return "#a3a3a3"; // neutral
  }
}

// --- Tier breakdown -------------------------------------------------------

function TierCard({
  data,
  err,
}: {
  data: TierCount[] | null | undefined;
  err: string | undefined;
}) {
  const rows = data ?? [];
  const total = rows.reduce((s, r) => s + r.count, 0);

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <Wallet className="size-4 text-muted-foreground" />
          <CardTitle className="text-base">Распределение по тарифам</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {err ? (
          <div className="text-sm text-destructive">Ошибка: {err}</div>
        ) : total === 0 ? (
          <EmptyHint text="Пока нет пользователей." />
        ) : (
          rows.map((row) => (
            <div key={row.tier} className="space-y-1">
              <div className="flex items-center justify-between text-sm">
                <span
                  className={cn(
                    "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium",
                    tierBadgeClass(row.tier as Tier),
                  )}
                >
                  {tierLabel(row.tier as Tier)}
                </span>
                <span className="tabular-nums text-muted-foreground">
                  {formatNumber(row.count)}
                </span>
              </div>
              <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className={cn(
                    "h-full rounded-full",
                    row.tier === "free"
                      ? "bg-muted-foreground/40"
                      : "bg-sky-400",
                  )}
                  style={{
                    width: total > 0 ? `${(row.count / total) * 100}%` : "0%",
                  }}
                />
              </div>
            </div>
          ))
        )}
      </CardContent>
    </Card>
  );
}

// --- Top servers ----------------------------------------------------------

function TopServersCard({
  data,
  err,
}: {
  data: TopServer[] | null | undefined;
  err: string | undefined;
}) {
  const rows = data ?? [];
  const max = rows.reduce((m, r) => Math.max(m, r.connection_count), 0);

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <ServerIcon className="size-4 text-muted-foreground" />
          <CardTitle className="text-base">Топ серверов за 30 дней</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        {err ? (
          <div className="text-sm text-destructive">Ошибка: {err}</div>
        ) : rows.length === 0 ? (
          <EmptyHint text="За последние 30 дней подключений не было." />
        ) : (
          <div className="space-y-3">
            {rows.map((row) => (
              <div key={row.server_id} className="space-y-1">
                <div className="flex items-center justify-between text-sm">
                  <div className="min-w-0 truncate font-medium">
                    {row.hostname}
                  </div>
                  <div className="shrink-0 tabular-nums text-muted-foreground">
                    {formatNumber(row.connection_count)}
                  </div>
                </div>
                <div className="flex items-center justify-between text-xs text-muted-foreground">
                  <div className="truncate">
                    {row.city}, {row.country}
                  </div>
                </div>
                <div className="h-1 w-full overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-sky-400"
                    style={{
                      width:
                        max > 0 ? `${(row.connection_count / max) * 100}%` : "0%",
                    }}
                  />
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// --- Shared bits ----------------------------------------------------------

function BarRow({
  label,
  count,
  total,
  color,
}: {
  label: string;
  count: number;
  total: number;
  color: string;
}) {
  const pct = total > 0 ? (count / total) * 100 : 0;
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-sm">
        <span>{label}</span>
        <span className="tabular-nums text-muted-foreground">
          {formatNumber(count)}
        </span>
      </div>
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
        <div
          className="h-full rounded-full"
          style={{ width: `${pct}%`, background: color }}
        />
      </div>
    </div>
  );
}

function LegendDot({ color, label }: { color: string; label: string }) {
  return (
    <div className="flex items-center gap-1.5">
      <span
        className="inline-block size-2 rounded-full"
        style={{ background: color }}
      />
      {label}
    </div>
  );
}

function EmptyHint({ text }: { text: string }) {
  return (
    <div className="rounded-md border border-dashed border-border p-4 text-center text-xs text-muted-foreground">
      {text}
    </div>
  );
}
