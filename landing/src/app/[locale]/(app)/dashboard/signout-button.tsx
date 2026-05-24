"use client";

import { useState, useTransition } from "react";
import { useTranslations } from "next-intl";
import { LogOut } from "lucide-react";
import { Dialog } from "@base-ui/react/dialog";

import { useRouter } from "@/i18n/navigation";
import { Button } from "@/components/ui/button";

/**
 * SignOutButton — the destructive-confirm sign-out control rendered in the
 * page header on /<locale>/dashboard (D-25).
 *
 * Flow:
 *   1. User clicks the "Sign out" trigger.
 *   2. base-ui Dialog opens with an i18n-keyed confirmation prompt.
 *   3. Confirm → POST /api/auth/logout (Plan 03's Node proxy endpoint).
 *      Plan 03 guarantees all three session cookies (rv_at/rv_rt/rv_user)
 *      are cleared even if the backend POST fails, so a transient backend
 *      outage cannot leave the browser "stuck signed in".
 *   4. router.replace("/") navigates back to the marketing home page; an
 *      explicit refresh() re-evaluates the (app) layout so NavbarApp drops
 *      back to its logged-out branch immediately.
 *
 * `useTransition` is used purely to give the button a pending state during
 * the post-logout navigation tick; the await fetch is intentionally OUTSIDE
 * `start()` so the network round-trip completes (and Set-Cookie clears
 * apply) BEFORE we trigger navigation.
 */
export function SignOutButton() {
  const t = useTranslations("auth.signOut.confirm");
  const tDash = useTranslations("dashboard");
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [pending, start] = useTransition();

  async function performSignOut() {
    try {
      await fetch("/api/auth/logout", {
        method: "POST",
        credentials: "same-origin",
      });
    } catch {
      // Plan 03's logout clears cookies even on backend failure; still navigate.
    }
    start(() => {
      router.replace("/");
      router.refresh();
    });
  }

  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Trigger
        render={
          <Button
            variant="ghost"
            size="sm"
            className="text-muted-foreground hover:text-destructive"
          >
            <LogOut className="h-4 w-4" />
            <span>{tDash("signOut")}</span>
          </Button>
        }
      />
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-50 bg-black/50 backdrop-blur" />
        <Dialog.Popup className="fixed left-1/2 top-1/2 z-50 -translate-x-1/2 -translate-y-1/2 rounded-[var(--radius-xl)] border border-border-subtle bg-surface-elevated p-6 w-[90vw] max-w-md">
          <Dialog.Title className="text-2xl font-semibold font-heading">
            {t("title")}
          </Dialog.Title>
          <Dialog.Description className="mt-2 text-sm text-muted-foreground">
            {t("body")}
          </Dialog.Description>
          <div className="mt-6 flex justify-end gap-2">
            <Dialog.Close render={<Button variant="ghost">{t("cancel")}</Button>} />
            <Button
              variant="destructive"
              disabled={pending}
              onClick={performSignOut}
            >
              {t("confirm")}
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
