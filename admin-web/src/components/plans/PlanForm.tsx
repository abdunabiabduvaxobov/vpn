import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { z } from "zod";
import type { AxiosError } from "axios";

import {
  createPlan,
  updatePlan,
  type AdminPlanDetail,
  type CreatePlanInput,
} from "@/api/plans";
import { Button } from "@/components/ui/button";
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

// codeSchema mirrors handler.validatePlanCode (plans_admin.go) — keep the
// regex literal exact. We enforce it client-side for UX only; the server
// re-validates on every write (defence in depth — UI is NOT authoritative).
const codeSchema = z
  .string()
  .min(1, "Минимум 1 символ")
  .max(40, "Максимум 40 символов")
  .regex(/^[a-z0-9][a-z0-9_-]*$/, "Только a-z, 0-9, _, -; первый символ — буква или цифра");

// The schema accepts -1 or 1..1000 for max_devices, -1 or 0..9999 for
// max_servers, 0..100000 for speed. Strings come in from <Input type="number">
// so we coerce.
const planFormSchema = z.object({
  code: codeSchema,
  name: z.string().min(1, "Минимум 1 символ").max(100, "Максимум 100 символов"),
  description: z.string().max(2000).optional().or(z.literal("")),
  max_devices: z.coerce
    .number()
    .int()
    .refine(
      (n) => n === -1 || (n >= 1 && n <= 1000),
      "Должно быть -1 (без лимита) или 1..1000",
    ),
  max_servers: z.coerce
    .number()
    .int()
    .refine(
      (n) => n === -1 || (n >= 0 && n <= 9999),
      "Должно быть -1 (без лимита) или 0..9999",
    ),
  speed_limit_mbps: z.coerce
    .number()
    .int()
    .min(0, "Минимум 0")
    .max(100000, "Максимум 100000"),
  sort_order: z.coerce.number().int().default(0),
  is_active: z.boolean().default(true),
});

type PlanFormValues = z.infer<typeof planFormSchema>;

export interface PlanFormProps {
  mode: "create" | "edit";
  plan?: AdminPlanDetail; // required in edit mode
}

export function PlanForm({ mode, plan }: PlanFormProps) {
  const qc = useQueryClient();
  const navigate = useNavigate();

  // Defaults: in create mode start with sensible Pro-style values; in edit
  // mode hydrate from the persisted plan.
  const form = useForm<PlanFormValues>({
    resolver: zodResolver(planFormSchema),
    defaultValues: plan
      ? {
          code: plan.code,
          name: plan.name,
          description: plan.description ?? "",
          max_devices: plan.max_devices,
          max_servers: plan.max_servers,
          speed_limit_mbps: plan.speed_limit_mbps,
          sort_order: plan.sort_order,
          is_active: plan.is_active,
        }
      : {
          code: "",
          name: "",
          description: "",
          max_devices: 5,
          max_servers: -1,
          speed_limit_mbps: 0,
          sort_order: 0,
          is_active: true,
        },
  });

  const createMut = useMutation({
    mutationFn: (values: PlanFormValues) => {
      // In create mode we don't ship any servers / offers from this form —
      // the operator wires those up via the Servers / Pricing tabs after
      // the plan exists. Initial create just plants the row.
      const input: CreatePlanInput = {
        code: values.code,
        name: values.name,
        description: values.description || undefined,
        max_devices: values.max_devices,
        max_servers: values.max_servers,
        speed_limit_mbps: values.speed_limit_mbps,
        sort_order: values.sort_order,
        server_ids: [],
        offers: [],
      };
      return createPlan(input);
    },
    onSuccess: async (created) => {
      await qc.invalidateQueries({ queryKey: ["admin", "plans"] });
      toast.success(`Тариф «${created.name}» создан`);
      // Navigate to the detail page so the operator can wire up servers
      // + offers immediately.
      navigate(`/plans/${created.id}`, { replace: true });
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(
        axiosErr.response?.data?.error ?? "Не удалось создать тариф",
      );
    },
  });

  const updateMut = useMutation({
    mutationFn: (values: PlanFormValues) => {
      if (!plan) throw new Error("no plan");
      // code is immutable post-creation (ADR §19.7.4) — never include it
      // in the PATCH body. The backend ALSO strips it.
      return updatePlan(plan.id, {
        name: values.name,
        description: values.description || "",
        max_devices: values.max_devices,
        max_servers: values.max_servers,
        speed_limit_mbps: values.speed_limit_mbps,
        sort_order: values.sort_order,
        is_active: values.is_active,
      });
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["admin", "plans"] });
      if (plan) {
        await qc.invalidateQueries({ queryKey: ["admin", "plan", plan.id] });
      }
      toast.success("Тариф обновлён");
    },
    onError: (err: unknown) => {
      const axiosErr = err as AxiosError<{ error?: string }>;
      toast.error(
        axiosErr.response?.data?.error ?? "Не удалось обновить тариф",
      );
    },
  });

  const busy = createMut.isPending || updateMut.isPending;

  // System plans cannot be deactivated (D-32 §4) — the backend returns 403
  // on PATCH{is_active:false}. Disable the switch in the UI as a hint.
  const isSystem = !!plan?.is_system;

  function onSubmit(values: PlanFormValues) {
    if (mode === "create") createMut.mutate(values);
    else updateMut.mutate(values);
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-6">
        <div className="grid gap-4 md:grid-cols-2">
          <FormField
            control={form.control}
            name="code"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Код (slug)</FormLabel>
                <FormControl>
                  {/* code is immutable in edit mode (ADR §19.7.4) — disabled
                      in the UI; the backend ALSO strips it from the body. */}
                  <Input
                    {...field}
                    placeholder="pro"
                    disabled={mode === "edit" || busy}
                    autoComplete="off"
                  />
                </FormControl>
                <FormDescription>
                  Стабильный идентификатор для интеграций. После создания
                  изменить нельзя.
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="name"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Название</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    placeholder="Pro"
                    disabled={busy}
                    autoComplete="off"
                  />
                </FormControl>
                <FormDescription>
                  Видно пользователю в /pricing.
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <FormField
          control={form.control}
          name="description"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Описание</FormLabel>
              <FormControl>
                <Textarea
                  {...field}
                  placeholder="Без ограничений по скорости, до 5 устройств…"
                  disabled={busy}
                  rows={3}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <div className="grid gap-4 md:grid-cols-3">
          <FormField
            control={form.control}
            name="max_devices"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Лимит устройств</FormLabel>
                <FormControl>
                  <Input
                    type="number"
                    inputMode="numeric"
                    {...field}
                    disabled={busy}
                  />
                </FormControl>
                <FormDescription>-1 = без лимита</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="max_servers"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Лимит серверов</FormLabel>
                <FormControl>
                  <Input
                    type="number"
                    inputMode="numeric"
                    {...field}
                    disabled={busy}
                  />
                </FormControl>
                <FormDescription>-1 = без лимита</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="speed_limit_mbps"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Скорость (Mbps)</FormLabel>
                <FormControl>
                  <Input
                    type="number"
                    inputMode="numeric"
                    {...field}
                    disabled={busy}
                  />
                </FormControl>
                <FormDescription>0 = без шейпинга</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <FormField
            control={form.control}
            name="sort_order"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Порядок сортировки</FormLabel>
                <FormControl>
                  <Input
                    type="number"
                    inputMode="numeric"
                    {...field}
                    disabled={busy}
                  />
                </FormControl>
                <FormDescription>
                  Меньше = выше в публичном /pricing.
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          {mode === "edit" && (
            <FormField
              control={form.control}
              name="is_active"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Активен</FormLabel>
                  <FormControl>
                    <div className="flex h-9 items-center gap-2">
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={busy || isSystem}
                      />
                      <span className="text-sm text-muted-foreground">
                        {field.value
                          ? "Доступен для подписки"
                          : "Скрыт из /pricing"}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {isSystem
                      ? "Системный тариф нельзя деактивировать (D-32 §4)."
                      : "Деактивированный тариф не предлагается новым подписчикам."}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-border pt-4">
          {mode === "create" && (
            <Button
              type="button"
              variant="outline"
              onClick={() => navigate("/plans")}
              disabled={busy}
            >
              Отмена
            </Button>
          )}
          <Button type="submit" disabled={busy}>
            {busy
              ? "Сохраняем…"
              : mode === "create"
                ? "Создать тариф"
                : "Сохранить"}
          </Button>
        </div>
      </form>
    </Form>
  );
}
