import { Navigate, Route, Routes } from "react-router-dom";

import { Suspense, lazy } from "react";

import { AdminLayout } from "@/components/layout/AdminLayout";
import { Dashboard } from "@/pages/Dashboard";
import { Login } from "@/pages/Login";
import { Users } from "@/pages/Users";
import { UserDetail } from "@/pages/UserDetail";

// Lazy-loaded routes — Servers, Activity and Settings are secondary
// pages that most admin sessions will never visit. Splitting them
// out of the initial bundle keeps the login → dashboard path lean.
const Servers = lazy(() =>
  import("@/pages/Servers").then((m) => ({ default: m.Servers })),
);
const Plans = lazy(() =>
  import("@/pages/Plans").then((m) => ({ default: m.Plans })),
);
const PlanDetail = lazy(() =>
  import("@/pages/PlanDetail").then((m) => ({ default: m.PlanDetail })),
);
const Activity = lazy(() =>
  import("@/pages/Activity").then((m) => ({ default: m.Activity })),
);
const Settings = lazy(() =>
  import("@/pages/Settings").then((m) => ({ default: m.Settings })),
);

function LazyFallback() {
  return (
    <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">
      Загрузка…
    </div>
  );
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route element={<AdminLayout />}>
        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<Dashboard />} />
        <Route path="/users" element={<Users />} />
        <Route path="/users/:id" element={<UserDetail />} />
        <Route
          path="/servers"
          element={
            <Suspense fallback={<LazyFallback />}>
              <Servers />
            </Suspense>
          }
        />
        <Route
          path="/plans"
          element={
            <Suspense fallback={<LazyFallback />}>
              <Plans />
            </Suspense>
          }
        />
        <Route
          path="/plans/:id"
          element={
            <Suspense fallback={<LazyFallback />}>
              <PlanDetail />
            </Suspense>
          }
        />
        <Route
          path="/activity"
          element={
            <Suspense fallback={<LazyFallback />}>
              <Activity />
            </Suspense>
          }
        />
        <Route
          path="/settings"
          element={
            <Suspense fallback={<LazyFallback />}>
              <Settings />
            </Suspense>
          }
        />
      </Route>
      <Route path="*" element={<Navigate to="/dashboard" replace />} />
    </Routes>
  );
}
