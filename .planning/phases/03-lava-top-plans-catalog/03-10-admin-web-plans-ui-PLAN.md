---
phase: 3
slug: lava-top-plans-catalog
plan_number: 10
wave: 5
depends_on: [5, 8]
files_modified:
  - admin-web/package.json
  - admin-web/package-lock.json
  - admin-web/src/components/ui/form.tsx
  - admin-web/src/components/ui/select.tsx
  - admin-web/src/components/ui/checkbox.tsx
  - admin-web/src/components/ui/switch.tsx
  - admin-web/src/components/ui/tabs.tsx
  - admin-web/src/components/ui/tooltip.tsx
  - admin-web/src/components/ui/textarea.tsx
  - admin-web/src/api/plans.ts
  - admin-web/src/api/lava.ts
  - admin-web/src/pages/Plans.tsx
  - admin-web/src/pages/PlanDetail.tsx
  - admin-web/src/components/plans/PlansTable.tsx
  - admin-web/src/components/plans/PlanForm.tsx
  - admin-web/src/components/plans/PlanServersPicker.tsx
  - admin-web/src/components/plans/PlanOffersGrid.tsx
  - admin-web/src/components/plans/LavaOfferPicker.tsx
  - admin-web/src/components/plans/DeletePlanDialog.tsx
  - admin-web/src/components/plans/ReplaceOfferDialog.tsx
  - admin-web/src/components/plans/PlanCodeBadge.tsx
  - admin-web/src/components/layout/AdminLayout.tsx
  - admin-web/src/App.tsx
autonomous: true
requirements_addressed: [PAY-13, PAY-14, PAY-15]
estimated_complexity: high
---

<objective>
Land the admin-web UI for plans + offers per ADR §19.13 (pulled into Phase 3 per D-13). Three steps: (1) install the 7 missing shadcn/Radix components (Form, Select, Checkbox, Switch, Tabs, Tooltip, Textarea), (2) write the API clients (`api/plans.ts`, `api/lava.ts`) typed against the handlers in plans 03-05 + 03-08, (3) write the 2 pages + 8 components per ADR §19.13.2-§19.13.4 + add the sidebar entry + route in App.tsx. The dropdown picker (D-12 Option B) sources offer IDs from `GET /admin/lava/products` — admin never types/pastes a UUID.
</objective>

<context>
@.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md
@.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md
@docs/ADR-007-lava-sso-rework.md
@admin-web/src/App.tsx
@admin-web/src/components/layout/AdminLayout.tsx
@admin-web/src/api/client.ts
@admin-web/src/api/servers.ts
@admin-web/src/pages/Servers.tsx
@admin-web/package.json
</context>

<interfaces>
API client types (`admin-web/src/api/plans.ts`) — match the Phase 3 backend response shapes from plans 03-05 + 03-08:

```ts
export interface AdminPlanSummary {
  id: string;
  code: string;
  name: string;
  description: string;
  max_devices: number;
  max_servers: number;
  speed_limit_mbps: number;
  is_active: boolean;
  is_system: boolean;
  sort_order: number;
  server_count: number;
  offer_count: number;
  active_user_count: number;
  created_at: string;
  updated_at: string;
}

export interface AdminPlanDetail extends AdminPlanSummary {
  servers: AdminServer[];
  offers: PlanOffer[];
}

export interface PlanOffer {
  id: string;
  plan_id: string;
  periodicity: string;
  currency: string;
  amount: number;
  lava_offer_id: string | null;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreatePlanInput {
  code: string;
  name: string;
  description?: string;
  max_devices: number;
  max_servers: number;
  speed_limit_mbps: number;
  sort_order?: number;
  server_ids: string[];
  offers: { periodicity: string; currency: string; amount: number; lava_offer_id?: string | null }[];
}
```

Mounted routes (admin-web/src/App.tsx):
```tsx
<Route path="/plans" element={<Plans />} />
<Route path="/plans/:id" element={<PlanDetail />} />
```

Sidebar entry (admin-web/src/components/layout/AdminLayout.tsx) — append to navItems:
```tsx
{ to: "/plans", label: "Тарифы", Icon: Tag },
```
</interfaces>

<tasks>

<task type="auto">
  <id>03-10-T01</id>
  <name>Wave 0: install 7 missing shadcn components (Form, Select, Checkbox, Switch, Tabs, Tooltip, Textarea)</name>
  <files>
    admin-web/package.json,
    admin-web/package-lock.json,
    admin-web/src/components/ui/form.tsx,
    admin-web/src/components/ui/select.tsx,
    admin-web/src/components/ui/checkbox.tsx,
    admin-web/src/components/ui/switch.tsx,
    admin-web/src/components/ui/tabs.tsx,
    admin-web/src/components/ui/tooltip.tsx,
    admin-web/src/components/ui/textarea.tsx
  </files>
  <read_first>
    - admin-web/package.json (CURRENT — Radix 1.1.6 baseline for existing dialog, dropdown-menu, label, separator, slot)
    - admin-web/src/components/ui/dialog.tsx (pattern for hand-vendored shadcn components)
    - admin-web/src/components/ui/dropdown-menu.tsx (similar Radix wrapper pattern)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §10.5 (component gap analysis — what each is used for)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-13 (admin UI in scope this phase)
  </read_first>
  <action>
    Step 1: from the `admin-web/` directory, install the underlying Radix primitives + react-hook-form (for `Form`) + zod (for validation):

    ```bash
    cd admin-web && npm install @radix-ui/react-checkbox@^1.1.2 @radix-ui/react-select@^2.1.4 @radix-ui/react-switch@^1.1.2 @radix-ui/react-tabs@^1.1.2 @radix-ui/react-tooltip@^1.1.6 react-hook-form@^7.54.0 @hookform/resolvers@^3.9.1 zod@^3.24.1
    ```

    Pin the versions explicitly (the carat-range matches the existing Radix 1.1.6 / 2.1.6 versions in package.json — sibling components from the same major). After install, package.json gains 7 new entries.

    Step 2: hand-vendor the 7 shadcn component files. Each follows the same structural pattern as the existing `dialog.tsx`/`dropdown-menu.tsx` — a thin TypeScript wrapper around the Radix primitive exposing `forwardRef`-wrapped components with Tailwind classes.

    The shadcn/ui canonical source for each component is `https://ui.shadcn.com/docs/components/{name}`. The 7 files are SHORT (under 100 lines each). The executor:

    1. Visit https://ui.shadcn.com/docs/components/form (and same for each component) and copy the canonical source verbatim into `admin-web/src/components/ui/{component}.tsx`.
    2. Adjust the import path: shadcn defaults to `@/lib/utils` for `cn` — this project HAS `@/lib/utils` (confirmed in existing `dialog.tsx`); the imports work as-is.
    3. For `form.tsx`, the canonical source imports `react-hook-form` — the install in Step 1 satisfies it.

    Files to create:
    - `admin-web/src/components/ui/form.tsx`
    - `admin-web/src/components/ui/select.tsx`
    - `admin-web/src/components/ui/checkbox.tsx`
    - `admin-web/src/components/ui/switch.tsx`
    - `admin-web/src/components/ui/tabs.tsx`
    - `admin-web/src/components/ui/tooltip.tsx`
    - `admin-web/src/components/ui/textarea.tsx`

    After all 7 files exist, run `cd admin-web && npm run lint && tsc --noEmit && npm run build`. Expect zero errors. If any component pulls in an additional dep (e.g. `cmdk` for combobox — but we're NOT installing combobox in this plan, Select is sufficient for the dropdown picker), install it.

    Step 3: Verify the file structure:
    ```bash
    ls admin-web/src/components/ui/
    ```
    expects 17 files now: the existing 10 + 7 new.
  </action>
  <acceptance_criteria>
    - `grep "@radix-ui/react-checkbox\|@radix-ui/react-select\|@radix-ui/react-switch\|@radix-ui/react-tabs\|@radix-ui/react-tooltip\|react-hook-form\|@hookform/resolvers\|zod" admin-web/package.json` finds at least 8 matches
    - All 7 new component files exist: `ls admin-web/src/components/ui/form.tsx admin-web/src/components/ui/select.tsx admin-web/src/components/ui/checkbox.tsx admin-web/src/components/ui/switch.tsx admin-web/src/components/ui/tabs.tsx admin-web/src/components/ui/tooltip.tsx admin-web/src/components/ui/textarea.tsx` succeeds
    - `cd admin-web && npm run lint` exits 0
    - `cd admin-web && tsc --noEmit` exits 0
    - `cd admin-web && npm run build` exits 0
  </acceptance_criteria>
  <automated>cd admin-web && tsc --noEmit && npm run build</automated>
  <done>7 shadcn components vendored; package.json has the Radix + react-hook-form + zod deps; admin-web builds clean.</done>
</task>

<task type="auto">
  <id>03-10-T02</id>
  <name>Write api/plans.ts + api/lava.ts (typed TanStack-Query fetchers for all 13 admin endpoints + lava products)</name>
  <files>
    admin-web/src/api/plans.ts,
    admin-web/src/api/lava.ts
  </files>
  <read_first>
    - admin-web/src/api/servers.ts (existing pattern — axios via @/api/client; envelope-unwrap interceptor)
    - admin-web/src/api/client.ts (the axios interceptor that strips `{data: ...}`)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §10.6 (api/plans.ts API surface — full list of functions verbatim)
    - server/api/internal/handler/plans_admin.go (plan 03-08 T01 — response shapes the client must match)
    - server/api/internal/handler/admin_lava.go (plan 03-05 T03 — lava products response shape)
  </read_first>
  <action>
    **(a) `admin-web/src/api/plans.ts`:**

```ts
import { api } from "@/api/client";

import type { AdminServer } from "@/api/servers";

export interface AdminPlanSummary {
  id: string;
  code: string;
  name: string;
  description: string;
  max_devices: number;
  max_servers: number;
  speed_limit_mbps: number;
  is_active: boolean;
  is_system: boolean;
  sort_order: number;
  server_count: number;
  offer_count: number;
  active_user_count: number;
  created_at: string;
  updated_at: string;
}

export interface PlanOffer {
  id: string;
  plan_id: string;
  periodicity: string; // ONE_TIME | MONTHLY | PERIOD_90_DAYS | PERIOD_180_DAYS | PERIOD_YEAR
  currency: string;    // USD | EUR | RUB
  amount: number;
  lava_offer_id: string | null;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface AdminPlanDetail extends AdminPlanSummary {
  servers: AdminServer[];
  offers: PlanOffer[];
}

export interface CreatePlanInput {
  code: string;
  name: string;
  description?: string;
  max_devices: number;
  max_servers: number;
  speed_limit_mbps: number;
  sort_order?: number;
  server_ids: string[];
  offers: Array<{
    periodicity: string;
    currency: string;
    amount: number;
    lava_offer_id?: string | null;
  }>;
}

export interface UpdatePlanInput {
  name?: string;
  description?: string;
  max_devices?: number;
  max_servers?: number;
  speed_limit_mbps?: number;
  sort_order?: number;
  is_active?: boolean;
}

export interface CreateOfferInput {
  periodicity: string;
  currency: string;
  amount: number;
  lava_offer_id?: string | null;
}

export interface UpdateOfferInput {
  amount?: number;
  lava_offer_id?: string | null;
  is_active?: boolean;
}

export interface ReplaceOfferInput {
  amount: number;
  lava_offer_id?: string | null;
}

export interface DeletePlanResult {
  id: string;
  deleted: boolean;
  affected_users: number;
}

const ROOT = "/api/v1/admin/plans";

export async function listPlans(): Promise<AdminPlanSummary[]> {
  const resp = await api.get<AdminPlanSummary[]>(ROOT);
  return resp.data;
}

export async function getPlan(id: string): Promise<AdminPlanDetail> {
  const resp = await api.get<AdminPlanDetail>(`${ROOT}/${id}`);
  return resp.data;
}

export async function createPlan(input: CreatePlanInput): Promise<AdminPlanDetail> {
  const resp = await api.post<AdminPlanDetail>(ROOT, input);
  return resp.data;
}

export async function updatePlan(id: string, input: UpdatePlanInput): Promise<AdminPlanDetail> {
  const resp = await api.patch<AdminPlanDetail>(`${ROOT}/${id}`, input);
  return resp.data;
}

export async function deletePlan(id: string, force?: boolean): Promise<DeletePlanResult> {
  const query = force ? "?force=true" : "";
  const resp = await api.delete<DeletePlanResult>(`${ROOT}/${id}${query}`);
  return resp.data;
}

export async function replacePlanServers(planId: string, serverIds: string[]): Promise<void> {
  await api.put(`${ROOT}/${planId}/servers`, { server_ids: serverIds });
}

export async function addPlanServer(planId: string, serverId: string): Promise<void> {
  await api.post(`${ROOT}/${planId}/servers/${serverId}`);
}

export async function removePlanServer(planId: string, serverId: string): Promise<void> {
  await api.delete(`${ROOT}/${planId}/servers/${serverId}`);
}

export async function listOffers(planId: string): Promise<PlanOffer[]> {
  const resp = await api.get<PlanOffer[]>(`${ROOT}/${planId}/offers`);
  return resp.data;
}

export async function createOffer(planId: string, input: CreateOfferInput): Promise<PlanOffer> {
  const resp = await api.post<PlanOffer>(`${ROOT}/${planId}/offers`, input);
  return resp.data;
}

export async function updateOffer(planId: string, offerId: string, input: UpdateOfferInput): Promise<PlanOffer> {
  const resp = await api.patch<PlanOffer>(`${ROOT}/${planId}/offers/${offerId}`, input);
  return resp.data;
}

export async function deleteOffer(planId: string, offerId: string): Promise<void> {
  await api.delete(`${ROOT}/${planId}/offers/${offerId}`);
}

export async function replaceOffer(planId: string, offerId: string, input: ReplaceOfferInput): Promise<PlanOffer> {
  const resp = await api.post<PlanOffer>(`${ROOT}/${planId}/offers/${offerId}/replace`, input);
  return resp.data;
}
```

    **(b) `admin-web/src/api/lava.ts`:**

```ts
import { api } from "@/api/client";

export interface LavaProduct {
  productId: string;
  productName: string;
  offerId: string;
  offerName: string;
  periodicity: string;
  currency: string;
  amount: number;
}

export async function listLavaProducts(): Promise<LavaProduct[]> {
  const resp = await api.get<LavaProduct[]>("/api/v1/admin/lava/products");
  return resp.data;
}
```

    Then `cd admin-web && tsc --noEmit && npm run lint`.
  </action>
  <acceptance_criteria>
    - Files `admin-web/src/api/plans.ts` and `admin-web/src/api/lava.ts` exist
    - `grep -c "^export async function" admin-web/src/api/plans.ts` returns at least 13
    - `grep "listLavaProducts" admin-web/src/api/lava.ts` finds one match
    - `grep "/api/v1/admin/lava/products" admin-web/src/api/lava.ts` finds one match
    - `grep -E "createPlan|updatePlan|deletePlan|replacePlanServers|addPlanServer|removePlanServer|createOffer|updateOffer|deleteOffer|replaceOffer" admin-web/src/api/plans.ts` finds at least 10 matches
    - `cd admin-web && tsc --noEmit` exits 0
    - `cd admin-web && npm run lint` exits 0
  </acceptance_criteria>
  <automated>cd admin-web && tsc --noEmit && npm run lint</automated>
  <done>plans.ts + lava.ts typed against backend handlers; 13 plans fetchers + 1 lava fetcher; clean tsc + lint.</done>
</task>

<task type="auto">
  <id>03-10-T03</id>
  <name>Write Plans.tsx (list view + New Plan CTA) + PlansTable.tsx + PlanCodeBadge.tsx + DeletePlanDialog.tsx</name>
  <files>
    admin-web/src/pages/Plans.tsx,
    admin-web/src/components/plans/PlansTable.tsx,
    admin-web/src/components/plans/PlanCodeBadge.tsx,
    admin-web/src/components/plans/DeletePlanDialog.tsx
  </files>
  <read_first>
    - admin-web/src/pages/Servers.tsx (existing precedent — TanStack Query useQuery + useMutation pattern)
    - admin-web/src/api/plans.ts (T02 of THIS plan)
    - docs/ADR-007-lava-sso-rework.md §19.13.1 (Plans table columns + row action menu), §19.13.4 (DeletePlanDialog modal — type plan code to confirm if active_user_count > 0)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-32 §4 (delete forbidden on system plans — UI shows 403 toast)
  </read_first>
  <action>
    Four new files. Pattern: page → uses TanStack Query → renders Table component → row action menu opens dialogs.

    **(a) `admin-web/src/pages/Plans.tsx`:** table view with "New Plan" CTA. Uses PlansTable for rendering.

    **(b) `admin-web/src/components/plans/PlansTable.tsx`:** columns per ADR §19.13.1 (Code badge, Name, Status pill, Servers count, Devices, Active users, Updated). Row action menu: Edit (Link to /plans/:id), Soft-delete (opens DeletePlanDialog). New Plan button at top-right routes to a "new plan" form (this plan defers form creation — admin opens detail page and fills in. Simpler MVP: just route to `/plans/new` and let PlanDetail handle the create-mode toggle in T04).

    **(c) `admin-web/src/components/plans/PlanCodeBadge.tsx`:** small read-only badge displaying the immutable code slug. Uses `<Badge>` from existing UI.

    **(d) `admin-web/src/components/plans/DeletePlanDialog.tsx`:** confirmation modal. When `active_user_count > 0`, requires the admin to type the plan code to confirm; calls `deletePlan(id, force=true)`. When `is_system=true`, the row action menu hides the delete option (server returns 403 anyway, but UI should not even offer it).

    Skeleton (executor expands; this is a multi-file TS UI plan — the implementation is mechanical):

```tsx
// admin-web/src/pages/Plans.tsx
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";

import { listPlans } from "@/api/plans";
import { Button } from "@/components/ui/button";
import { PlansTable } from "@/components/plans/PlansTable";

export function Plans() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ["admin", "plans"],
    queryFn: listPlans,
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Тарифы</h1>
        <Button asChild>
          <Link to="/plans/new">+ Новый тариф</Link>
        </Button>
      </div>
      {isLoading && <div>Загрузка…</div>}
      {isError && <div className="text-destructive">Не удалось загрузить тарифы</div>}
      {data && <PlansTable plans={data} />}
    </div>
  );
}
```

```tsx
// admin-web/src/components/plans/PlansTable.tsx
import { Link } from "react-router-dom";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

import type { AdminPlanSummary } from "@/api/plans";
import { PlanCodeBadge } from "./PlanCodeBadge";
import { DeletePlanDialog } from "./DeletePlanDialog";

export function PlansTable({ plans }: { plans: AdminPlanSummary[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Код</TableHead>
          <TableHead>Название</TableHead>
          <TableHead>Статус</TableHead>
          <TableHead className="text-right">Серверы</TableHead>
          <TableHead className="text-right">Устройства</TableHead>
          <TableHead className="text-right">Активных пользователей</TableHead>
          <TableHead>Обновлён</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {plans.map((p) => (
          <TableRow key={p.id}>
            <TableCell><PlanCodeBadge code={p.code} /></TableCell>
            <TableCell>{p.name}</TableCell>
            <TableCell>
              {p.is_system ? <Badge variant="secondary">System</Badge>
                : p.is_active ? <Badge>Active</Badge>
                : <Badge variant="outline">Inactive</Badge>}
            </TableCell>
            <TableCell className="text-right">{p.server_count}</TableCell>
            <TableCell className="text-right">{p.max_devices === -1 ? "∞" : p.max_devices}</TableCell>
            <TableCell className="text-right">{p.active_user_count}</TableCell>
            <TableCell>{new Date(p.updated_at).toLocaleString()}</TableCell>
            <TableCell className="text-right">
              <Button variant="ghost" size="sm" asChild>
                <Link to={`/plans/${p.id}`}>Изменить</Link>
              </Button>
              {!p.is_system && (
                <DeletePlanDialog plan={p} />
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
```

```tsx
// admin-web/src/components/plans/PlanCodeBadge.tsx
import { Badge } from "@/components/ui/badge";

export function PlanCodeBadge({ code }: { code: string }) {
  return <Badge variant="secondary" className="font-mono">{code}</Badge>;
}
```

```tsx
// admin-web/src/components/plans/DeletePlanDialog.tsx
import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { AxiosError } from "axios";

import { deletePlan } from "@/api/plans";
import type { AdminPlanSummary } from "@/api/plans";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function DeletePlanDialog({ plan }: { plan: AdminPlanSummary }) {
  const [open, setOpen] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const qc = useQueryClient();

  const mutation = useMutation({
    mutationFn: () => deletePlan(plan.id, plan.active_user_count > 0),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["admin", "plans"] });
      toast.success(`Тариф «${plan.name}» удалён`);
      setOpen(false);
      setConfirmation("");
    },
    onError: (err) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(axiosErr.response?.data?.error ?? "Не удалось удалить тариф");
    },
  });

  const needsConfirmation = plan.active_user_count > 0;
  const canConfirm = needsConfirmation ? confirmation === plan.code : true;

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="ghost" size="sm">Удалить</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Удалить тариф «{plan.name}»?</DialogTitle>
          <DialogDescription>
            {needsConfirmation
              ? `${plan.active_user_count} пользователей на этом тарифе. После удаления они останутся на нём до окончания оплаченного периода, затем перейдут на системный тариф.`
              : "Тариф будет помечен как неактивный; восстановить можно через прямой SQL-доступ."}
          </DialogDescription>
        </DialogHeader>
        {needsConfirmation && (
          <div className="space-y-2">
            <Label htmlFor="confirm-code">Введите код тарифа для подтверждения:</Label>
            <Input
              id="confirm-code"
              value={confirmation}
              onChange={(e) => setConfirmation(e.target.value)}
              placeholder={plan.code}
            />
          </div>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={() => setOpen(false)}>Отмена</Button>
          <Button
            variant="destructive"
            disabled={!canConfirm || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? "Удаление…" : "Удалить"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

    Run `cd admin-web && tsc --noEmit && npm run build`.
  </action>
  <acceptance_criteria>
    - 4 new files exist
    - `grep "useQuery" admin-web/src/pages/Plans.tsx` finds at least one match
    - `grep "useMutation\\|deletePlan" admin-web/src/components/plans/DeletePlanDialog.tsx` finds matches
    - `grep "PlanCodeBadge" admin-web/src/components/plans/PlansTable.tsx` finds matches
    - `grep "is_system" admin-web/src/components/plans/PlansTable.tsx` finds matches (D-32 §4 — UI hides delete for system plans)
    - `cd admin-web && tsc --noEmit` exits 0
    - `cd admin-web && npm run build` exits 0
  </acceptance_criteria>
  <automated>cd admin-web && tsc --noEmit && npm run build</automated>
  <done>Plans listing page + table + code badge + delete dialog all wired; system plans hide the delete affordance.</done>
</task>

<task type="auto">
  <id>03-10-T04</id>
  <name>Write PlanDetail.tsx (three tabs) + PlanForm.tsx + PlanServersPicker.tsx + PlanOffersGrid.tsx + LavaOfferPicker.tsx + ReplaceOfferDialog.tsx</name>
  <files>
    admin-web/src/pages/PlanDetail.tsx,
    admin-web/src/components/plans/PlanForm.tsx,
    admin-web/src/components/plans/PlanServersPicker.tsx,
    admin-web/src/components/plans/PlanOffersGrid.tsx,
    admin-web/src/components/plans/LavaOfferPicker.tsx,
    admin-web/src/components/plans/ReplaceOfferDialog.tsx
  </files>
  <read_first>
    - admin-web/src/api/plans.ts + admin-web/src/api/lava.ts (T02 of THIS plan)
    - admin-web/src/api/servers.ts (existing — for ListServers in PlanServersPicker)
    - admin-web/src/components/ui/tabs.tsx + form.tsx + select.tsx + checkbox.tsx + switch.tsx + tooltip.tsx (T01 — UI primitives)
    - docs/ADR-007-lava-sso-rework.md §19.13.2 (three-tab form — Limits, Servers, Pricing), §19.13.3 (server-side data dependencies), §19.13.4 (replace offer confirmation modal)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-12 (LAVA_offer_id ONLY via dropdown — no paste), D-13 (admin UI in scope)
  </read_first>
  <action>
    Six files. The architecture: `/plans/:id` route renders PlanDetail; PlanDetail uses Tabs to switch between Limits / Servers / Pricing. Each tab is a component.

    Skeleton (executor expands the full implementations):

    **(a) `admin-web/src/pages/PlanDetail.tsx`:**

```tsx
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { getPlan } from "@/api/plans";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PlanForm } from "@/components/plans/PlanForm";
import { PlanServersPicker } from "@/components/plans/PlanServersPicker";
import { PlanOffersGrid } from "@/components/plans/PlanOffersGrid";

export function PlanDetail() {
  const { id } = useParams();
  const isNew = id === "new";
  const { data: plan, isLoading } = useQuery({
    queryKey: ["admin", "plan", id],
    queryFn: () => getPlan(id!),
    enabled: !isNew && !!id,
  });

  if (isNew) {
    return <PlanForm mode="create" />;
  }
  if (isLoading || !plan) return <div>Загрузка…</div>;

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">{plan.name}</h1>
      <Tabs defaultValue="limits">
        <TabsList>
          <TabsTrigger value="limits">Лимиты</TabsTrigger>
          <TabsTrigger value="servers">Серверы</TabsTrigger>
          <TabsTrigger value="pricing">Цены</TabsTrigger>
        </TabsList>
        <TabsContent value="limits"><PlanForm mode="edit" plan={plan} /></TabsContent>
        <TabsContent value="servers"><PlanServersPicker planId={plan.id} initial={plan.servers} /></TabsContent>
        <TabsContent value="pricing"><PlanOffersGrid planId={plan.id} initial={plan.offers} /></TabsContent>
      </Tabs>
    </div>
  );
}
```

    **(b) `admin-web/src/components/plans/PlanForm.tsx`** — react-hook-form + zod schema for the Limits tab. Code field is disabled in edit mode (immutable). Validates code regex `^[a-z0-9][a-z0-9_-]*$`; max_devices/-1 or 1..1000; max_servers/-1 or 0..9999; speed_limit_mbps 0..100000; name 1..100.

    **(c) `admin-web/src/components/plans/PlanServersPicker.tsx`** — split-pane checkbox grid. Fetches `/api/v1/admin/servers` (existing endpoint); groups by `country`. Uses `replacePlanServers(planId, serverIds)` on save. Shows the warning banner from ADR §19.13.2 Tab 2 when removing a server that was previously selected.

    **(d) `admin-web/src/components/plans/PlanOffersGrid.tsx`** — grid of (periodicity × currency) cells. Each cell either shows the offer amount + lava_offer_id mark + a kebab menu (Edit / Replace / Deactivate) OR a "+" button to create. Editing opens a modal that contains LavaOfferPicker.

    **(e) `admin-web/src/components/plans/LavaOfferPicker.tsx`** — `<Select>` of lava products from `listLavaProducts()`. **D-12 enforced**: no paste path; admin must pick from the dropdown. Shows a tooltip when amount < $5 / €5 ("lava floor is $5/€5").

    **(f) `admin-web/src/components/plans/ReplaceOfferDialog.tsx`** — confirmation modal for price changes. Shows old amount → new amount diff. Requires explicit checkbox before Save. Calls `replaceOffer(planId, offerId, {amount, lava_offer_id})`.

    Each of these components is ~50-100 lines. Executor implements them following the patterns in `admin-web/src/pages/Servers.tsx` (TanStack Query useMutation, sonner toasts, axios error mapping).

    Then `cd admin-web && tsc --noEmit && npm run build`.
  </action>
  <acceptance_criteria>
    - All 6 files exist
    - `grep "TabsTrigger" admin-web/src/pages/PlanDetail.tsx` finds at least 3 matches (Limits, Servers, Pricing)
    - `grep "react-hook-form\\|useForm" admin-web/src/components/plans/PlanForm.tsx` finds matches
    - `grep "disabled" admin-web/src/components/plans/PlanForm.tsx` finds matches (code disabled in edit mode per ADR §19.7.4)
    - `grep "listLavaProducts" admin-web/src/components/plans/LavaOfferPicker.tsx` finds one match (D-12 source)
    - `grep "Select\\|SelectItem" admin-web/src/components/plans/LavaOfferPicker.tsx` finds at least 2 matches (D-12 dropdown, no paste)
    - `grep "replaceOffer" admin-web/src/components/plans/ReplaceOfferDialog.tsx` finds one match (PAY-15)
    - `grep "replacePlanServers" admin-web/src/components/plans/PlanServersPicker.tsx` finds matches (PAY-14)
    - `cd admin-web && tsc --noEmit` exits 0
    - `cd admin-web && npm run build` exits 0
  </acceptance_criteria>
  <automated>cd admin-web && tsc --noEmit && npm run build</automated>
  <done>PlanDetail with 3 tabs; PlanForm/PlanServersPicker/PlanOffersGrid + LavaOfferPicker (D-12 dropdown-only) + ReplaceOfferDialog (PAY-15 modal); all components build clean.</done>
</task>

<task type="auto">
  <id>03-10-T05</id>
  <name>Wire /plans + /plans/:id routes in App.tsx; add "Тарифы" sidebar entry in AdminLayout.tsx</name>
  <files>
    admin-web/src/App.tsx,
    admin-web/src/components/layout/AdminLayout.tsx
  </files>
  <read_first>
    - admin-web/src/App.tsx (CURRENT — lazy-loaded route pattern for Servers/Activity/Settings)
    - admin-web/src/components/layout/AdminLayout.tsx (CURRENT — navItems array)
    - admin-web/src/pages/Plans.tsx + admin-web/src/pages/PlanDetail.tsx (T03 + T04 of THIS plan)
  </read_first>
  <action>
    **(a) Edit `admin-web/src/App.tsx`:**

    Add lazy imports:
```tsx
const Plans = lazy(() =>
  import("@/pages/Plans").then((m) => ({ default: m.Plans })),
);
const PlanDetail = lazy(() =>
  import("@/pages/PlanDetail").then((m) => ({ default: m.PlanDetail })),
);
```

    Add routes inside the `<Route element={<AdminLayout />}>` block (alongside existing /servers + /activity + /settings):
```tsx
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
```

    **(b) Edit `admin-web/src/components/layout/AdminLayout.tsx`:**

    Add to the `navItems` array (after Servers, before Activity per ADR §19.13.5):
```tsx
  { to: "/plans", label: "Тарифы", Icon: Tag },
```

    Add `Tag` to the lucide-react imports at the top of the file.

    Then `cd admin-web && tsc --noEmit && npm run build`.
  </action>
  <acceptance_criteria>
    - `grep "import.*Tag" admin-web/src/components/layout/AdminLayout.tsx` finds one match (icon import)
    - `grep "/plans" admin-web/src/components/layout/AdminLayout.tsx` finds one match
    - `grep "Тарифы" admin-web/src/components/layout/AdminLayout.tsx` finds one match
    - `grep "Plans\\|PlanDetail" admin-web/src/App.tsx` finds at least 4 matches (2 lazy imports + 2 routes)
    - `grep 'path="/plans"' admin-web/src/App.tsx` finds one match
    - `grep 'path="/plans/:id"' admin-web/src/App.tsx` finds one match
    - `cd admin-web && tsc --noEmit && npm run build` exits 0
  </acceptance_criteria>
  <automated>cd admin-web && tsc --noEmit && npm run build</automated>
  <done>/plans + /plans/:id routes mounted in App.tsx; sidebar has "Тарифы" entry between Servers and Activity per ADR §19.13.5.</done>
</task>

</tasks>

<verification>
- `cd admin-web && tsc --noEmit && npm run lint && npm run build` exits 0
- All 7 missing shadcn components vendored under `admin-web/src/components/ui/`
- 13 typed fetchers exist in `admin-web/src/api/plans.ts`; 1 in `admin-web/src/api/lava.ts`
- Plans page + PlanDetail page + 7 plan components exist
- Sidebar entry "Тарифы" + 2 routes wired
- Operator can navigate to `/plans` → see table → click row → see PlanDetail with 3 tabs (manual verification — UI flow per ADR §19.13)
</verification>

<must_haves>
truths:
  - "Admin opens /plans and sees a table of all plans with code badge, name, status pill, server count, devices, active user count, updated time."
  - "Admin clicks Edit → /plans/:id with three tabs: Limits (PlanForm), Servers (PlanServersPicker), Pricing (PlanOffersGrid)."
  - "Plans table hides the delete affordance on system plans (D-32 §4 — UI mirrors backend 403)."
  - "DeletePlanDialog requires typing the plan code to confirm when active_user_count > 0 (ADR §19.13.4)."
  - "PlanOffersGrid → Edit cell opens modal with LavaOfferPicker — D-12 enforced: lava_offer_id sourced ONLY from /admin/lava/products dropdown, no paste path."
  - "ReplaceOfferDialog shows old → new amount diff + explicit confirmation checkbox (ADR §19.13.4); calls POST /admin/plans/:id/offers/:offer_id/replace (PAY-15)."
  - "code field is disabled in PlanForm edit mode (ADR §19.7.4 — immutable post-creation)."
  - "Sidebar has 'Тарифы' entry between Servers and Activity per ADR §19.13.5."
  - "All 7 missing shadcn components installed: Form, Select, Checkbox, Switch, Tabs, Tooltip, Textarea (Combobox not strictly required — Select suffices for D-12 dropdown)."
artifacts:
  - path: "admin-web/src/api/plans.ts"
    provides: "13 typed fetchers for /admin/plans/*"
    contains: "replaceOffer"
  - path: "admin-web/src/api/lava.ts"
    provides: "listLavaProducts for D-12 dropdown source"
    contains: "/api/v1/admin/lava/products"
  - path: "admin-web/src/pages/PlanDetail.tsx"
    provides: "Three-tab plan editor (Limits / Servers / Pricing)"
    contains: "TabsTrigger"
  - path: "admin-web/src/components/plans/LavaOfferPicker.tsx"
    provides: "D-12 dropdown-only offer picker"
    contains: "listLavaProducts"
key_links:
  - from: "admin-web/src/components/plans/LavaOfferPicker.tsx"
    to: "admin-web/src/api/lava.ts::listLavaProducts"
    via: "Dropdown source (D-12 — no paste path)"
    pattern: "listLavaProducts"
  - from: "admin-web/src/App.tsx"
    to: "admin-web/src/pages/PlanDetail.tsx"
    via: "React Router /plans/:id"
    pattern: 'path="/plans/:id"'
</must_haves>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| Admin browser → admin-web | Admin JWT in cookie/localStorage; all API calls authenticated. |
| admin-web → backend | All form submissions validated server-side too (defence in depth — UI validation NOT authoritative). |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-74 | Information disclosure | API key leaks via /admin/lava/products to browser | mitigate | The proxy endpoint runs server-side with the admin API key; only the dropdown rows (productId/offerId/etc) reach the browser. Verified in plan 03-05 T03 threat model. |
| T-03-75 | Elevation of Privilege | Admin pastes a UUID into lava_offer_id field via DOM tampering | mitigate | D-12: there IS no paste input. The LavaOfferPicker's `<Select>` is bound to `listLavaProducts()` results — the form value comes from the dropdown's onChange. Even if a determined admin modifies the DOM via DevTools, the BACKEND (plan 03-08) doesn't validate that lava_offer_id was sourced from the dropdown — it just stores the UUID. So this is a UX guard, not a security boundary. The real defence is the lava-side: if the offer UUID doesn't exist on lava.top, CreateInvoice fails. |
| T-03-76 | Tampering | Admin sends `{"is_system":true}` via curl bypassing the UI | mitigate | Backend handler struct doesn't have is_system field (plan 03-08 T01). UI is a convenience; backend is the boundary. |
| T-03-77 | Repudiation | Admin denies making a change | mitigate | AuditLog middleware (extended in plan 03-08 T03) records every write with admin user_id + action label + request body. UI shows the audit log via Phase 7 ADMIN-06 (deferred). |
| T-03-78 | DoS | Admin clicks delete repeatedly | accept | TanStack Query's mutation is single-fired (isPending blocks the button). Backend rate limit also applies. |

ASVS L1 scoping for the admin-web side (UI is convenience). All security boundaries live in the backend — verified by plans 03-05, 03-06, 03-08 threat models.
</threat_model>

<success_criteria>
1. `cd admin-web && tsc --noEmit && npm run lint && npm run build` exits 0.
2. All 7 missing shadcn components vendored.
3. 13 admin-plan fetchers + 1 lava-products fetcher in api/.
4. /plans page renders the table with row action menus.
5. /plans/:id page renders 3 tabs (Limits, Servers, Pricing).
6. Sidebar entry "Тарифы" added in lexical position per ADR §19.13.5.
7. PAY-13/14/15 UI surfaces all exist and call the matching backend endpoints.
</success_criteria>

<output>
T01..T05 land as 5 atomic commits (`feat(03-10): ...`); planner commits this plan file once with `docs(03): plan admin-web-plans-ui`.
</output>
