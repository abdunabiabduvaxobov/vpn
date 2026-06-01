import { Suspense, lazy } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  AlertTriangle,
  BadgeDollarSign,
  CalendarDays,
  CreditCard,
  Radio,
  Server,
  TrendingDown,
  UserPlus,
  Users,
} from "lucide-react";

import { getAdminStats, type AdminStats } from "@/api/stats";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { formatNumber } from "@/lib/format";

// Recharts is heavy (~100 KB gz). Splitting the chart-bearing sections
// into their own chunks keeps the initial dashboard render fast —
// users see the KPI cards immediately and the charts fade in a moment
// later. Vite hoists shared deps (recharts) between these two lazy
// chunks into a common chunk automatically.
const StatsChart = lazy(() =>
  import("@/components/StatsChart").then((m) => ({ default: m.StatsChart })),
);
const AnalyticsSection = lazy(() =>
  import("@/components/AnalyticsSection").then((m) => ({
    default: m.AnalyticsSection,
  })),
);

interface KpiDef {
  key: keyof AdminStats;
  label: string;
  Icon: React.ComponentType<{ className?: string }>;
  // format defaults to a plain ru-RU number; "currency" renders MRR as a
  // USD figure (the backend defaults the MRR currency to USD).
  format?: "number" | "currency";
}

// formatMRR renders the MRR float as a compact USD amount. The backend
// resolves MRR per-currency (default USD); the dashboard shows the default.
function formatMRR(n: number | null | undefined): string {
  if (n == null) return "—";
  return new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: 0,
  }).format(n);
}

// The KPI bar maps straight onto AdminStats. The first row is the original
// fleet/account summary; the rest are the ADMIN-01 revenue/growth KPIs.
// active_connections is rendered from its own fast-refresh query (below) so
// it can update every 15s without dropping the global 60s cadence.
const kpis: KpiDef[] = [
  { key: "total_users", label: "Всего пользователей", Icon: Users },
  {
    key: "active_subscriptions",
    label: "Активные подписки",
    Icon: CreditCard,
  },
  { key: "active_server_count", label: "Активные серверы", Icon: Activity },
  { key: "server_count", label: "Всего серверов", Icon: Server },
  { key: "paid_users", label: "Платящие", Icon: BadgeDollarSign },
  { key: "mrr", label: "MRR (USD)", Icon: BadgeDollarSign, format: "currency" },
  { key: "signups_today", label: "Регистрации сегодня", Icon: UserPlus },
  { key: "signups_week", label: "Регистрации за неделю", Icon: CalendarDays },
  { key: "signups_month", label: "Регистрации за месяц", Icon: CalendarDays },
  { key: "churn_30d", label: "Отток за 30 дней", Icon: TrendingDown },
  {
    key: "failed_payments_30d",
    label: "Сбои оплат за 30 дней",
    Icon: AlertTriangle,
  },
];

function renderKpi(
  def: KpiDef,
  data: AdminStats | undefined,
  isLoading: boolean,
  isError: boolean,
): string {
  if (isLoading) return "…";
  if (isError) return "—";
  if (def.format === "currency") return formatMRR(data?.[def.key]);
  return formatNumber(data?.[def.key]);
}

export function Dashboard() {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "stats"],
    queryFn: getAdminStats,
    refetchInterval: 60_000,
  });

  // active_connections is the most volatile KPI; poll it on a tighter 15s
  // cadence on its own query key so live tunnel count stays fresh without
  // re-running the heavier full-stats query every 15s.
  const liveConns = useQuery({
    queryKey: ["admin", "stats", "active-connections"],
    queryFn: getAdminStats,
    refetchInterval: 15_000,
    select: (s) => s.active_connections,
  });

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Обзор</h1>
        <p className="text-sm text-muted-foreground">
          Актуальная сводка по пользователям, подпискам и VPN-серверам.
        </p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {/* Live active-connections card — its own 15s query key. */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Активные подключения
            </CardTitle>
            <Radio className="size-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-semibold">
              {liveConns.isLoading
                ? "…"
                : liveConns.isError
                  ? "—"
                  : formatNumber(liveConns.data)}
            </div>
          </CardContent>
        </Card>
        {kpis.map((def) => (
          <Card key={def.key}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">
                {def.label}
              </CardTitle>
              <def.Icon className="size-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-semibold">
                {renderKpi(def, data, isLoading, isError)}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {isError && (
        <Card className="border-destructive/40 bg-destructive/10">
          <CardContent className="p-4 text-sm text-destructive">
            Не удалось загрузить статистику: {(error as Error).message}
          </CardContent>
        </Card>
      )}

      <Suspense fallback={<Skeleton className="h-[340px] w-full" />}>
        <StatsChart />
      </Suspense>

      <Suspense fallback={<Skeleton className="h-[600px] w-full" />}>
        <AnalyticsSection />
      </Suspense>
    </div>
  );
}
