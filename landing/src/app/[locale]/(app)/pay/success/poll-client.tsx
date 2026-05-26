"use client";

/**
 * PollClient — invoice polling state machine for /pay/success (Plan 04-07 / WEB-06).
 *
 * Polling contract (CONTEXT D-21 — DO NOT change these numbers without revising D-21):
 *   - Cadence: 2000 ms (INTERVAL_MS)
 *   - Polls 1-5: GET /api/v1/invoices/:id           (cheap path, backend cache only)
 *   - Polls 6+:  GET /api/v1/invoices/:id?escalate=true   (force backend → lava lookup)
 *   - Hard timeout: 30000 ms total — after this, surface the "takingLonger" UI
 *
 * Status mapping (backend status, lowercased — backend can return either casing
 * per 03-05 SUMMARY: `mapLavaStatusToLocal` normalises both):
 *
 *   - "paid"      → force-refresh (B2/D-17 — see below) → "active" UI
 *   - "pending"   → keep polling (no UI change)
 *   - "failed"    → redirect to /pay/fail?reason=declined
 *   - "cancelled" → redirect to /pay/fail?reason=cancelled
 *   - unknown     → keep polling (transient — treat unknown as pending)
 *
 * 401 → /login?next=/dashboard (session expired during the polling window).
 * 404 → /pay/fail?reason=default (invoice not owned by caller — backend's
 *       ownership check returns 404, not 403, per 03-05).
 *
 * B2/D-17 — The forced-refresh trigger.
 * ============================================================================
 * When polling observes `status === "paid"`, the backend has already upgraded
 * users.plan_id, but the user's current rv_at JWT was minted BEFORE the
 * upgrade and still claims the old plan_id. We POST to /api/v1/auth/refresh
 * through the Plan 03 proxy — the proxy intercepts that path, calls the
 * backend, decodes plan_id from the new access_token, and atomically re-issues
 * rv_at + rv_rt + rv_user (with the upgraded planId) on the response.
 *
 * By the time we navigate to /dashboard the rv_user cookie reflects "pro" —
 * exactly the milestone goal "land on /pay/success with Pro already active".
 *
 * Failure handling for the refresh: errors are SWALLOWED. The cookie side-
 * effects are the only thing that matters; even if the refresh fails (network
 * blip, transient backend hiccup), the natural rv_at expiry (≤5 min) will
 * pick up the new plan_id on the next normal request. The "active" UI still
 * renders so the user knows their payment went through — the dashboard might
 * just show "Free" for one refresh cycle in that worst case.
 */

import { useEffect, useRef, useState } from "react";

import { useRouter } from "@/i18n/navigation";
import { PaymentStatusCard } from "@/components/app/payment-status-card";

type Props = { invoiceId: string; locale: string };

// D-21 — locked polling parameters. Changing these requires revising D-21.
const INTERVAL_MS = 2000;
const ESCALATE_AFTER_POLL = 6;
const TIMEOUT_MS = 30000;

export function PollClient({ invoiceId }: Props) {
  const router = useRouter();
  const [view, setView] = useState<"processing" | "active" | "takingLonger">(
    "processing",
  );
  const pollNo = useRef(0);
  const timerRef = useRef<number | null>(null);
  const timeoutRef = useRef<number | null>(null);
  const stopped = useRef(false);

  function stop() {
    stopped.current = true;
    if (timerRef.current !== null) window.clearInterval(timerRef.current);
    if (timeoutRef.current !== null) window.clearTimeout(timeoutRef.current);
  }

  /**
   * B2/D-17 — force the Plan 03 proxy to rotate rv_at/rv_rt and re-issue
   * rv_user with the upgraded plan_id BEFORE we flip the UI to "active".
   * Errors swallowed (see file-level docstring).
   */
  async function forceRefreshForNewPlanId() {
    try {
      await fetch("/api/v1/auth/refresh", {
        method: "POST",
        credentials: "same-origin",
        signal: AbortSignal.timeout(5000),
      });
    } catch {
      // Non-fatal — natural rv_at expiry within 5 min picks up the new plan_id.
    }
  }

  async function pollOnce() {
    if (stopped.current) return;
    pollNo.current += 1;
    const useEscalate = pollNo.current >= ESCALATE_AFTER_POLL;
    const url = `/api/v1/invoices/${encodeURIComponent(invoiceId)}${
      useEscalate ? "?escalate=true" : ""
    }`;
    try {
      const r = await fetch(url, { credentials: "same-origin" });
      if (r.status === 401) {
        stop();
        router.replace("/login?next=/dashboard");
        return;
      }
      if (r.status === 404) {
        stop();
        router.replace(
          `/pay/fail?invoiceId=${encodeURIComponent(invoiceId)}&reason=default`,
        );
        return;
      }
      if (!r.ok) {
        // Transient (5xx, rate-limit) — keep polling.
        return;
      }
      const json = await r.json().catch(() => null);
      // Backend may return uppercase OR lowercase status — normalise before
      // comparing. Mirrors backend's mapLavaStatusToLocal symmetry.
      const status = (json?.status ?? "").toString().toLowerCase();
      if (status === "paid") {
        stop();
        // B2/D-17 — force refresh BEFORE flipping to active so /dashboard
        // reads the fresh planId on the user's next navigation.
        await forceRefreshForNewPlanId();
        setView("active");
        return;
      }
      if (status === "failed") {
        stop();
        router.replace(
          `/pay/fail?invoiceId=${encodeURIComponent(invoiceId)}&reason=declined`,
        );
        return;
      }
      if (status === "cancelled") {
        stop();
        router.replace(
          `/pay/fail?invoiceId=${encodeURIComponent(invoiceId)}&reason=cancelled`,
        );
        return;
      }
      // "pending" or unknown → keep polling.
    } catch {
      // Network blip — keep polling. The 30s timeout is the hard backstop.
    }
  }

  useEffect(() => {
    // Reset the one-shot lifecycle refs at the TOP of the effect body so
    // React Strict Mode's simulated unmount → remount cycle does not leave
    // stopped.current=true (set by the first pass's cleanup → stop()) and
    // strand pollOnce() in an early-return branch. Without these resets,
    // SC#4 happy and SC#4 timeout both fail under NODE_ENV=development.
    stopped.current = false;
    pollNo.current = 0;
    // Kick the first poll immediately so the user sees a transition at ~t=0
    // rather than waiting INTERVAL_MS for the first network call.
    pollOnce();
    timerRef.current = window.setInterval(pollOnce, INTERVAL_MS);
    timeoutRef.current = window.setTimeout(() => {
      if (stopped.current) return;
      if (timerRef.current !== null) window.clearInterval(timerRef.current);
      setView("takingLonger");
    }, TIMEOUT_MS);
    return () => stop();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  /**
   * Manual refresh after the 30s timeout. Single-shot — does NOT restart the
   * timer. If this shot lands "paid", we still force-refresh so the navigation
   * to /dashboard reflects the new plan_id (same B2/D-17 contract as pollOnce).
   */
  function refresh() {
    (async () => {
      const url = `/api/v1/invoices/${encodeURIComponent(invoiceId)}?escalate=true`;
      try {
        const r = await fetch(url, { credentials: "same-origin" });
        if (!r.ok) return;
        const json = await r.json().catch(() => null);
        const status = (json?.status ?? "").toString().toLowerCase();
        if (status === "paid") {
          await forceRefreshForNewPlanId();
          setView("active");
        } else if (status === "failed") {
          router.replace(
            `/pay/fail?invoiceId=${encodeURIComponent(invoiceId)}&reason=declined`,
          );
        } else if (status === "cancelled") {
          router.replace(
            `/pay/fail?invoiceId=${encodeURIComponent(invoiceId)}&reason=cancelled`,
          );
        }
        // "pending" / unknown → leave the takingLonger UI in place.
      } catch {
        // Swallow — UI already in takingLonger; user can click Refresh again.
      }
    })();
  }

  if (view === "processing") {
    return <PaymentStatusCard state="processing" invoiceId={invoiceId} />;
  }
  if (view === "active") {
    return <PaymentStatusCard state="active" invoiceId={invoiceId} />;
  }
  return (
    <PaymentStatusCard
      state="takingLonger"
      invoiceId={invoiceId}
      onRefresh={refresh}
    />
  );
}
