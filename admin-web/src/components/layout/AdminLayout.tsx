import { NavLink, Navigate, Outlet, useNavigate } from "react-router-dom";
import {
  Activity,
  CreditCard,
  HeartPulse,
  LayoutDashboard,
  LogOut,
  Server,
  Settings,
  ShieldCheck,
  Tag,
  Users,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { shortId } from "@/lib/format";
import { useAuthStore } from "@/stores/authStore";

const navItems = [
  { to: "/dashboard", label: "Обзор", Icon: LayoutDashboard },
  { to: "/users", label: "Пользователи", Icon: Users },
  { to: "/servers", label: "Серверы", Icon: Server },
  { to: "/plans", label: "Тарифы", Icon: Tag },
  { to: "/payments", label: "Платежи", Icon: CreditCard },
  { to: "/system", label: "Система", Icon: HeartPulse },
  { to: "/activity", label: "Журнал", Icon: Activity },
  { to: "/settings", label: "Настройки", Icon: Settings },
];

export function AdminLayout() {
  const tokens = useAuthStore((s) => s.tokens);
  const user = useAuthStore((s) => s.user);
  const clear = useAuthStore((s) => s.clear);
  const navigate = useNavigate();

  // Auth guard — no tokens means route straight to /login. This is the
  // only guard the app needs because /login is the only public route.
  if (!tokens || !user) {
    return <Navigate to="/login" replace />;
  }

  function handleLogout() {
    clear();
    navigate("/login", { replace: true });
  }

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      <aside className="flex w-60 flex-col border-r border-border bg-card">
        <div className="flex items-center gap-2 px-6 py-5">
          <ShieldCheck className="size-5 text-primary" />
          <span className="text-sm font-semibold tracking-wide">
            VPN — админка
          </span>
        </div>
        <Separator />
        <nav className="flex-1 space-y-1 px-3 py-4">
          {navItems.map(({ to, label, Icon }) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-accent text-accent-foreground"
                    : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
                )
              }
            >
              <Icon className="size-4" />
              {label}
            </NavLink>
          ))}
        </nav>
        <Separator />
        <div className="px-4 py-4 text-xs text-muted-foreground">
          <div className="truncate font-medium text-foreground">
            {user.full_name}
          </div>
          <div className="truncate">ID {shortId(user.id)}</div>
          <div className="capitalize">{user.role}</div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center justify-between border-b border-border px-6">
          <div className="text-sm text-muted-foreground">
            vpnadmin.mydayai.uz
          </div>
          <Button variant="ghost" size="sm" onClick={handleLogout}>
            <LogOut className="size-4" />
            Выйти
          </Button>
        </header>
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
