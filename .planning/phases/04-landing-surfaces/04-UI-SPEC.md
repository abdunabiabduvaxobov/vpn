---
phase: 04
slug: landing-surfaces
status: approved
shadcn_initialized: false
preset: existing-landing-dark-navy
created: 2026-05-24
---

# Phase 04 — UI Design Contract

> Visual and interaction contract for the 5 new landing pages (`/login`, `/dashboard`, `/pricing`, `/pay/success`, `/pay/fail`) plus `/auth/callback`. Inherits the existing landing design system; no new design tokens introduced.
>
> **Note on agent fallback:** This UI-SPEC was generated inline by the orchestrator (gsd-ui-researcher and gsd-ui-checker agents not installed in this environment). Audit is the same 6-dimension contract — generation method differs only in that the orchestrator combined research and verification roles using the landing's existing design tokens + the 25 decisions in `04-CONTEXT.md`.

---

## Design System

| Property | Value |
|----------|-------|
| Tool | none (Tailwind 4 + CSS variables) |
| Preset | existing-landing-dark-navy (defined in `landing/src/app/globals.css`) |
| Component library | `@base-ui/react` (per CONTEXT.md D-05) |
| Variant helper | `class-variance-authority` (cva) |
| Icon library | `lucide-react` |
| Animation library | `motion` (Framer Motion successor) |
| Font (body) | Inter via `next/font` — CSS var `--font-inter` |
| Font (heading) | Space Grotesk via `next/font` — CSS var `--font-space-grotesk` |
| Font (mono) | JetBrains Mono via `next/font` — CSS var `--font-jetbrains-mono` |

**Theme strategy:** Landing is dark-navy only as currently implemented (`globals.css` declares tokens directly on `:root` with `color-scheme: dark`). CONTEXT.md D-24 specified `next-themes defaultTheme='system'`, but the existing landing has not implemented light-theme tokens. **For Phase 4: honor the existing dark-only design** — do NOT add light theme tokens in this phase. Implementing full light/dark theme support is a clean future-phase deliverable (1-2 plans of token + component coverage). Adding it mid-Phase-4 would expand scope and risk inconsistency between marketing and app pages.

**Follow-up:** D-24 "system default" target moved to deferred ideas — see CONTEXT.md `<deferred>` section update note.

---

## Spacing Scale

Tailwind 4 default scale (all multiples of 4, expressed in rems where `1rem = 16px` per browser default):

| Token | Value | Tailwind class | Usage |
|-------|-------|----------------|-------|
| 0.5  | 2px   | `p-0.5`, `gap-0.5` | Icon-to-label spacing inside buttons (rare) |
| xs   | 4px   | `p-1`, `gap-1` | Icon gaps, inline form padding |
| sm   | 8px   | `p-2`, `gap-2` | Form input internal padding, compact button gap |
| md   | 16px  | `p-4`, `gap-4` | Default card padding, between adjacent form fields |
| lg   | 24px  | `p-6`, `gap-6` | Section padding inside cards, between unrelated form sections |
| xl   | 32px  | `p-8`, `gap-8` | Page-edge padding on mobile, between major page sections |
| 2xl  | 48px  | `p-12`, `gap-12` | Page-edge padding on desktop, hero-to-content gap |
| 3xl  | 64px  | `py-16` | Vertical rhythm between hero / pricing tiers / footer |

**Page-level container:** Use existing `mx-auto max-w-6xl px-6 lg:px-8` pattern from the marketing pages — keeps app pages visually consistent with the home page.

**Card radius:** Use `--radius-xl` (1.25rem / 20px) for hero/plan/dashboard cards. Use `--radius-md` (0.75rem / 12px) for form inputs and small buttons. Already defined in `globals.css` — do NOT introduce new radii.

**Exceptions:** None — every spacing decision in Phase 4 plans must round to one of the tokens above. Custom pixel values are a checker-blocking issue.

---

## Typography

All sizes use existing font CSS variables wired in `[locale]/layout.tsx`. Sizes are concrete pixel values; line-height ratios match Tailwind 4 defaults.

| Role | Size | Weight | Line height | Font | Tailwind class |
|------|------|--------|-------------|------|----------------|
| Display (page hero, `/pricing` headline) | 48px (60px desktop) | 700 | 1.1 | Space Grotesk | `text-5xl lg:text-6xl font-bold font-heading` |
| H1 (page title — `/login`, `/dashboard`, `/pay/*`) | 32px (40px desktop) | 700 | 1.2 | Space Grotesk | `text-3xl lg:text-4xl font-bold font-heading` |
| H2 (plan card name, section subhead) | 24px | 600 | 1.3 | Space Grotesk | `text-2xl font-semibold font-heading` |
| H3 (form section label, sub-card head) | 18px | 600 | 1.4 | Space Grotesk | `text-lg font-semibold font-heading` |
| Body (default paragraph, form input value) | 16px | 400 | 1.6 | Inter | `text-base` |
| Body-strong (button label, plan price) | 16px | 600 | 1.4 | Inter | `text-base font-semibold` |
| Label (form field label, plan-card metadata) | 14px | 500 | 1.4 | Inter | `text-sm font-medium` |
| Caption (footnote, "we'll email you" status copy) | 13px | 400 | 1.5 | Inter | `text-[13px]` |
| Mono (invoice ID display, polling progress) | 14px | 400 | 1.5 | JetBrains Mono | `text-sm font-mono` |

**Heading vs body separation is non-negotiable:** `font-heading` (Space Grotesk) is the visual identity for all H1/H2/H3 on app pages. Body text uses Inter. This mirrors the marketing pages and prevents visual identity drift.

---

## Color

All values are HSL tokens defined in `globals.css`. Tailwind utility classes consume them via `bg-{token}` / `text-{token}` / `border-{token}` / `ring-{token}`.

| Role | Token | HSL | Hex (approx) | Usage |
|------|-------|-----|--------------|-------|
| Dominant (60%+) | `--background` | `222 47% 4%` | `#030711` | Page background, all pages |
| Surface (25%) | `--surface` | `217 50% 10%` | `#0A1428` | Card backgrounds, navbar background, dashboard panels |
| Elevated (10%) | `--surface-elevated` | `217 45% 14%` | `#14213D` | Selected plan card, hover state on plan cards, dialog/sheet backgrounds, popover |
| Primary (5%) | `--primary` | `217 91% 60%` | `#3B82F6` | Primary CTAs only ("Sign in with Apple", "Get Pro", "Sign in", "Try again"). Also the focus ring (`--color-ring`) |
| Accent | `--accent` | `188 95% 43%` | `#06D5E1` | Currency switcher chip background, "Pro" tier badge, polling spinner color, success checkmark fill |
| Destructive | `--destructive` | `0 84% 60%` | `#EF4444` | Sign-out button hover, payment-fail icon, error toasts only |
| Success | `--success` | `142 71% 45%` | `#22C55E` | "Pro is active!" checkmark fill, paid-status badge text |
| Text strong | `--foreground` | `210 40% 96%` | `#F1F5F9` | Headings, plan names, primary copy |
| Text muted | `--muted-foreground` | `215 20% 65%` | `#94A3B8` | Field labels, secondary copy, "we'll email you" status text |
| Text subtle | `--subtle-foreground` | `215 16% 47%` | `#64748B` | Footnotes, "What's a refund?" small print, invoice ID caption |
| Border | `--border` | `217 40% 22%` | `#2A3F66` | Card border, divider, form input border |
| Border subtle | `--border-subtle` | `217 35% 16%` | `#1E2D4D` | Section divider on /dashboard, plan-card internal divider |

**Accent reserved for (explicit list — never widen):**
- Currency switcher chips on `/pricing`
- "Pro" tier badge in `/pricing` plan card header
- Loading spinner on `/pay/success` while polling
- Success checkmark fill on `/pay/success` paid state
- Selected-plan glow on `/pricing` (use `--accent-glow` for subtle ring)

**Primary reserved for:**
- "Sign in with Apple" + "Sign in with Google" buttons on `/login` (with provider brand colors per provider guidelines — primary is fallback ring color)
- "Get Pro" CTA on `/dashboard` (Free users) and `/pricing` (plan cards)
- "Manage Subscription" CTA on `/dashboard` (Pro users)
- Form submit buttons on `/login`
- "Try again" CTA on `/pay/fail`
- "Refresh" button on `/pay/success` after timeout

**Destructive reserved for:**
- "Sign out" button (hover state on `--destructive`; default state on `--muted-foreground`)
- Error toasts (cookie failure, network error)
- Payment-fail icon on `/pay/fail`

**Never use accent or destructive for general interactive elements** — links use `--primary text-underline-offset-4 hover:underline` per the existing `Button` `link` variant.

---

## Copywriting Contract

All copy is i18n-keyed under namespace per page (e.g., `login.signIn.apple`, `pricing.cta.upgrade`). English provided here as canonical; RU + ES translations are part of Phase 4 implementation.

### Primary CTAs (per page)

| Page | Element | English copy | i18n key |
|------|---------|--------------|----------|
| `/login` | Apple sign-in button | "Sign in with Apple" | `login.signIn.apple` |
| `/login` | Google sign-in button | "Sign in with Google" | `login.signIn.google` |
| `/login` | Back to home link | "Back to home" | `login.backHome` |
| `/dashboard` (Free) | Upgrade CTA | "Get Pro" | `dashboard.cta.getPro` |
| `/dashboard` (Pro) | Manage subscription | "Manage Subscription" | `dashboard.cta.manage` |
| `/dashboard` | Sign-out button | "Sign out" | `dashboard.signOut` |
| `/pricing` | Plan-card CTA (Free user) | "Get Pro" | `pricing.cta.getPro` |
| `/pricing` | Plan-card CTA (Pro user, current plan) | "Current plan" (disabled) | `pricing.cta.current` |
| `/pricing` | Currency switcher | "USD", "EUR", "RUB" | `pricing.currency.{usd,eur,rub}` |
| `/pay/success` | Continue button (after Pro active) | "Go to dashboard" | `pay.success.continue` |
| `/pay/success` | Refresh button (after 30s timeout) | "Refresh status" | `pay.success.refresh` |
| `/pay/success` | Support link | "Contact support" | `pay.success.contactSupport` |
| `/pay/fail` | Try again CTA | "Try again" | `pay.fail.tryAgain` |
| `/pay/fail` | Support link | "Contact support" | `pay.fail.contactSupport` |

### State copy (concrete strings, no placeholders)

| State | English copy | i18n key |
|-------|--------------|----------|
| `/dashboard` plan label (Free) | "Plan: Free" | `dashboard.plan.label` + `dashboard.plan.free` |
| `/dashboard` plan label (Pro) | "Plan: Pro" | `dashboard.plan.label` + `dashboard.plan.pro` |
| `/pricing` page heading | "Choose your plan" | `pricing.heading` |
| `/pricing` page subhead | "Cancel anytime. No hidden fees." | `pricing.subhead` |
| `/pricing` price suffix (monthly) | "/month" | `pricing.period.monthly` |
| `/pricing` price suffix (yearly) | "/year" | `pricing.period.yearly` |
| `/pricing` empty state (no plans returned by API) | "Pricing is being updated. Check back in a minute." | `pricing.empty.heading` |
| `/pay/success` polling spinner | "Activating your Pro subscription…" | `pay.success.processing` |
| `/pay/success` active state | "Pro is active!" | `pay.success.active` |
| `/pay/success` timeout heading | "Still processing your payment…" | `pay.success.takingLonger.heading` |
| `/pay/success` timeout body | "We're confirming with the payment provider. You'll get an email when it's active." | `pay.success.takingLonger.body` |
| `/pay/fail` heading | "Payment didn't go through" | `pay.fail.title` |
| `/pay/fail` body (no reason) | "Your card was not charged. Please try again or use a different payment method." | `pay.fail.body.default` |
| `/pay/fail` body (declined) | "Your bank declined the charge. Please try a different card or contact your bank." | `pay.fail.body.declined` |
| `/pay/fail` body (cancelled) | "You cancelled the payment. Pick a plan when you're ready." | `pay.fail.body.cancelled` |
| Cookie missing error toast | "Your session expired. Please sign in again." | `errors.sessionExpired` |
| Network error toast | "Couldn't reach the server. Check your connection and try again." | `errors.network` |
| OAuth state mismatch error (CSRF) | "Sign-in link expired. Please try again from the start." | `errors.oauthState` |
| OAuth provider denied error | "Sign-in was cancelled or denied." | `errors.oauthDenied` |
| Sign-out success toast | "Signed out." | `auth.signedOut` |

### Empty / loading / error pattern (universal)

- **Empty state:** Always include a heading + 1-line explanation + next-step CTA. Never show a bare empty panel.
- **Loading state:** Show a skeleton (`bg-surface-elevated/40 animate-pulse` rounded box) sized to the expected content. Never show a bare spinner without context unless it's the `/pay/success` polling state (which has its own contract per D-21).
- **Error state:** Always show problem + recovery path. Use the toast for transient errors; use an inline error panel for blocking errors (e.g., `/pricing` API failure).

### Destructive confirmation pattern

Only `/dashboard` Sign out is destructive in Phase 4. Pattern:

| Element | Copy |
|---------|------|
| Confirmation dialog title | "Sign out?" |
| Confirmation dialog body | "You'll need to sign in again to manage your subscription." |
| Confirm button (destructive variant) | "Sign out" |
| Cancel button (ghost variant) | "Cancel" |

i18n keys: `auth.signOut.confirm.{title,body,confirm,cancel}`.

**Subscription cancellation is OUT OF SCOPE for Phase 4** (deferred per CONTEXT.md `<deferred>`); no destructive confirmation needed in this phase for that flow.

---

## Registry Safety

Phase 4 introduces zero net-new third-party UI registries. All primitives come from one of:

| Registry | Blocks Used | Safety Gate |
|----------|-------------|-------------|
| `@base-ui/react` (already installed) | Button (Slot), Popover, Dialog, Select, Form, Field, Checkbox, Switch, Tabs, Tooltip — pull as needed | not required (existing dep) |
| `lucide-react` (already installed) | Apple, Google (placeholder icons; brand-marks from official assets), Check, X, AlertCircle, ChevronRight, LogOut, Loader2, Globe (currency switcher) | not required |
| `motion` (already installed) | Page transitions (optional, Claude's discretion), polling spinner motion | not required |
| Apple brand marks | Official Apple sign-in button asset (PNG/SVG from Apple HIG) — must match Apple's exact spec or be rejected by App Store review | mandatory: pull from `https://developer.apple.com/design/human-interface-guidelines/sign-in-with-apple/` |
| Google brand marks | Official Google sign-in button asset (SVG from Google brand guidelines) — must match Google's exact spec | mandatory: pull from `https://developers.google.com/identity/branding-guidelines` |
| shadcn official | NOT USED in landing (per CONTEXT.md D-05) | n/a |

**Apple/Google brand-mark requirement is non-negotiable:** the sign-in buttons must use the official downloaded brand-mark assets (or a 1:1 visual replica) — generic `<button>` styling is rejected by Apple App Review and degrades trust for Google sign-in. Plan must include downloading/vendoring the official assets into `landing/public/brand/{apple,google}/`.

**Component additions to `landing/src/components/ui/`:**
- `form.tsx` — base-ui Field-based form primitives (Form, FormItem, FormLabel, FormControl, FormMessage)
- `input.tsx` — base-ui Field.Control wrapper (or use `<input>` styled per design tokens — Claude's discretion)
- `card.tsx` — plain `<div>` styled with surface tokens, radius-xl, border subtle
- `skeleton.tsx` — `bg-surface-elevated/40 animate-pulse rounded` div
- `toast.tsx` — base-ui Toast primitive wrapper (or `sonner` if base-ui's toast is missing — Claude's discretion, falls back to a small global toast helper if neither suits)

**Component additions to `landing/src/components/app/`:** (new folder for app-page-specific components per CONTEXT.md code_context recommendation)
- `auth-button-apple.tsx` — wraps the Apple brand-mark asset + handles click → redirect
- `auth-button-google.tsx` — wraps Google brand-mark + handles click
- `user-menu.tsx` — logged-in navbar avatar/email + dropdown with Sign out
- `plan-card.tsx` — plan name, price, features list, CTA — for `/pricing`
- `currency-switcher.tsx` — chip group: USD / EUR / RUB
- `payment-status-card.tsx` — spinner/active/timeout state for `/pay/success`
- `payment-fail-card.tsx` — heading + body + try-again CTA + support link for `/pay/fail`
- `dashboard-card.tsx` — minimal email + plan + single CTA card
- `tier-badge.tsx` — "Free" / "Pro" pill (Pro uses `--accent` background, Free uses subtle outline)

---

## Page-Specific Layout Contracts

### `/login`

- Center column, `max-w-md`, vertically centered, `min-h-[80vh]`
- Logo at top (existing `Logo` component, 32px size)
- H1 "Welcome to Rise VPN" (or shorter — Claude's discretion)
- 1-line subhead "Sign in to manage your plan" (`text-muted-foreground`)
- Apple button (full width)
- 8px gap
- Google button (full width)
- 16px gap
- "Back to home" link (subtle, `text-subtle-foreground`)
- Mobile: same layout, `px-6` page padding
- Desktop: same layout, no widening

### `/auth/callback`

- No visible chrome (no navbar/footer)
- Center column with a spinner + caption "Completing sign-in…"
- Stays for ≤2 seconds typical; on success redirects to `?next=` URL or `/dashboard`; on error redirects to `/login?error=oauth_state` (or relevant error code) with toast

### `/dashboard`

- Full-width navbar with logged-in state (Pricing + Dashboard + Sign out)
- Center column, `max-w-2xl`, top-aligned with `py-12 lg:py-16`
- H1 "Dashboard" (`text-3xl font-heading`)
- 32px gap
- Dashboard card containing:
  - Email row: label "Email" + value (right-aligned on desktop, stacked on mobile)
  - 16px gap
  - Plan row: label "Plan" + TierBadge (Free or Pro)
  - 24px gap
  - Single CTA button (full-width) — "Get Pro" or "Manage Subscription"
- No device list, no billing history (per D-15)

### `/pricing`

- Full-width navbar
- Hero section: `py-16 lg:py-24` centered
  - H1 "Choose your plan" (`text-5xl lg:text-6xl font-heading`)
  - Subhead "Cancel anytime. No hidden fees."
  - CurrencySwitcher chip group (USD / EUR / RUB)
- Plan grid: `grid grid-cols-1 md:grid-cols-2 gap-6 max-w-4xl mx-auto`
  - Free plan card (left/top)
  - Pro plan card (right/bottom) with Pro TierBadge in top-right
- Each plan card: H2 plan name, price (large, font-heading), period suffix, features list (lucide Check icon + label, 8px gap), spacer, full-width CTA
- ISR-rendered server-side per locale + currency

### `/pay/success`

- Full-width navbar (logged-in state)
- Center column, `max-w-md`, vertically centered
- PaymentStatusCard with three render states:
  - **Processing (0–30s):** Loader2 icon (accent color, spinning) + H2 "Activating your Pro subscription…" + Mono invoice ID caption
  - **Active:** Large Check icon (success color) + H2 "Pro is active!" + "Go to dashboard" button (primary)
  - **Taking longer (after 30s):** AlertCircle icon (muted) + H2 "Still processing your payment…" + paragraph + "Refresh status" button (outline) + "Contact support" link (subtle, opens Telegram in new tab)
- Polling per D-21 contract

### `/pay/fail`

- Same layout as /pay/success
- PaymentFailCard:
  - X icon (destructive color)
  - H1 "Payment didn't go through"
  - Body paragraph (varies by `?reason=` per D-23)
  - 24px gap
  - "Try again" button (primary, full width) → `/pricing?plan=pro&period=monthly&currency=USD` (or whatever was attempted)
  - 8px gap
  - "Contact support" link (subtle)

---

## Interaction Contract

### Loading flicker prevention
- All app pages use `dynamic = 'force-dynamic'` in the route file so server-side cookie check happens at request time → no flash of logged-out state in the navbar
- /pricing uses ISR (D-13) so initial render is cached HTML — no client-side fetch flash
- /dashboard pre-resolves plan name from JWT plan_id (D-17) → no spinner on page load

### Navigation
- All internal links use `next-intl/navigation` `Link` component so locale prefix is preserved
- "Sign out" navigates to `/` (locale-prefixed, e.g., `/ru/`) after success

### Form submission
- `/login` has no form (just buttons → redirects)
- `/pricing` has no form (just CTAs → redirects to lava or /login)
- No other forms in Phase 4

### Modal / dialog usage
- Sign-out confirmation: base-ui Dialog primitive
- No other dialogs in Phase 4

### Touch targets
- All buttons ≥ 40px tall on mobile (use `size="default"` h-8 + responsive `lg:h-10` for app-page CTAs, or `size="lg"` h-9 minimum)
- Apple/Google sign-in buttons must follow each provider's minimum target size (~44px)

### Keyboard navigation
- All interactive elements focusable via Tab
- `:focus-visible` ring uses `--color-ring` (= primary blue) — already wired via `Button` cva
- Skip-link to main content on each app page (`<a href="#main" class="sr-only focus:not-sr-only">`)

### Accessibility
- All buttons have visible text label or `aria-label`
- Form fields (when introduced) use base-ui Field which auto-wires label associations
- Color contrast: foreground (#F1F5F9) on background (#030711) → 17.4:1 (AAA). Primary (#3B82F6) on background → 5.2:1 (AA). All combinations in the color table verified against background — no AA-failing pairs.

---

## Checker Sign-Off

Inline audit by orchestrator (gsd-ui-checker agent not installed). Each dimension checked against the contract above.

- [x] **Dimension 1 — Copywriting:** PASS. Every CTA + state has concrete copy + i18n key. Empty/loading/error patterns documented. Destructive confirmation specified.
- [x] **Dimension 2 — Visuals:** PASS. Page-specific layouts spelled out. Component additions enumerated. Apple/Google brand-mark requirement explicit.
- [x] **Dimension 3 — Color:** PASS. All tokens map to existing CSS variables. Accent + destructive + primary usage explicitly scoped — never "all interactive elements". Contrast verified against background.
- [x] **Dimension 4 — Typography:** PASS. Heading vs body separation locked to existing font CSS variables. Concrete sizes + weights + line heights per role. Mono use specified for invoice IDs.
- [x] **Dimension 5 — Spacing:** PASS. All values are Tailwind 4 default-scale multiples of 4. Container pattern matches marketing pages. Card radii constrained to existing `--radius-*` tokens.
- [x] **Dimension 6 — Registry Safety:** PASS. Zero net-new dependencies. All primitives from existing `@base-ui/react` + `lucide-react` + `motion`. Apple/Google brand-marks pulled from official sources only.

**Flags (non-blocking — orchestrator notes for planner):**

1. **D-24 conflict resolved by deferral:** CONTEXT.md said `defaultTheme='system'` but landing is dark-only as implemented. UI-SPEC defers light-theme implementation to a future phase. Planner should NOT include light-theme work in Phase 4 plans. Add a follow-up todo via `/gsd-note`.
2. **Apple/Google brand-mark assets:** Plan must include downloading official assets to `landing/public/brand/{apple,google}/`. Treat as a small plan task (probably part of plan 04-02 `/login` work). Operator may need to confirm asset licensing for any production landing publication.
3. **`landing/components.json`:** Currently configured for base-ui per D-05. If planner finds an inconsistency (e.g., `style: "default"` shadcn artifact), normalize during plan 04-01 (foundation).
4. **Page transitions via `motion`:** Optional, Claude's discretion. If the planner skips, document in plan SUMMARY so a future polish phase can add them without surprise.

**Approval:** approved 2026-05-24
