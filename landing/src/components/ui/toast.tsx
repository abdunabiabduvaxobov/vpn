"use client";

import { Toast } from "@base-ui/react/toast";

import { cn } from "@/lib/utils";

/**
 * Toast — thin wrapper around @base-ui/react/toast (v1.4.1).
 *
 * Phase 4 D-05 keeps us on base-ui (no shadcn). We expose:
 *   - <Toaster /> — drop into a layout once; mounts Provider + Portal +
 *     Viewport + Positioner + Root.
 *   - toast({ title, description, variant }) — fire-and-forget helper any
 *     client component can call without `useToastManager()` boilerplate.
 *
 * Implementation choice: we create a SINGLE module-scope ToastManager via
 * `createToastManager()` and pass it to <Toast.Provider toastManager={...}>.
 * That way `toast()` calls from any module reach the same provider instance,
 * which is the documented pattern for cross-component invocation in
 * @base-ui/react 1.x (vs the in-tree `useToastManager()` hook that only works
 * inside a Provider subtree).
 */

export type ToastVariant = "default" | "destructive" | "success";

const manager = Toast.createToastManager();

interface ToastOptions {
  title?: string;
  description?: string;
  variant?: ToastVariant;
  timeout?: number;
}

export function toast({
  title,
  description,
  variant = "default",
  timeout = 5000,
}: ToastOptions): string {
  return manager.add({
    title,
    description,
    type: variant,
    timeout,
  });
}

export function Toaster() {
  return (
    <Toast.Provider toastManager={manager}>
      <Toast.Portal>
        <Toast.Viewport className="pointer-events-none fixed top-4 right-4 z-[100] flex w-[min(360px,calc(100vw-2rem))] flex-col gap-2">
          <ToastList />
        </Toast.Viewport>
      </Toast.Portal>
    </Toast.Provider>
  );
}

function ToastList() {
  const { toasts } = Toast.useToastManager();
  return (
    <>
      {toasts.map((t) => {
        const variant = (t.type as ToastVariant | undefined) ?? "default";
        return (
          <Toast.Root
            key={t.id}
            toast={t}
            className={cn(
              "pointer-events-auto flex flex-col gap-1 rounded-[var(--radius-lg)] border border-border-subtle bg-surface-elevated p-4 text-sm text-foreground shadow-lg transition-all data-[ending-style]:opacity-0 data-[starting-style]:translate-x-4 data-[starting-style]:opacity-0",
              variant === "destructive" &&
                "border-destructive/40 bg-destructive/10 text-destructive",
              variant === "success" && "border-accent/40 bg-accent/10",
            )}
          >
            {t.title ? (
              <Toast.Title className="font-medium">{t.title}</Toast.Title>
            ) : null}
            {t.description ? (
              <Toast.Description className="text-muted-foreground">
                {t.description}
              </Toast.Description>
            ) : null}
            <Toast.Close
              aria-label="Close notification"
              className="absolute top-2 right-2 inline-flex h-6 w-6 items-center justify-center rounded-full text-muted-foreground transition hover:text-foreground"
            >
              <svg
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                className="h-3 w-3"
                aria-hidden="true"
              >
                <line x1="18" y1="6" x2="6" y2="18" />
                <line x1="6" y1="6" x2="18" y2="18" />
              </svg>
            </Toast.Close>
          </Toast.Root>
        );
      })}
    </>
  );
}
