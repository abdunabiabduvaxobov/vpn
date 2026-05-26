# Phase 5: Mobile SSO + Pro CTA - Context

**Gathered:** 2026-05-26
**Status:** Ready for planning
**Source:** /gsd-discuss-phase interactive session

<domain>
## Phase Boundary

Ship a React Native mobile build (v2.2.0) where users sign in with Apple, Google, or Guest; PaymentScreen becomes informational-only with one CTA pointing to `risevpn.com/<locale>/pricing`; the deep link `vpnapp://payment/success?invoiceId=X` returns the user to the app and Pro is reflected on Home within 5 seconds. Backend SSO (`/auth/apple`, `/auth/google`, guest→identified promotion) is already shipped in Phase 2; the web payment loop (`/pay/success` polling, "Open in app" deep-link emit) is already shipped in Phase 4. **This phase is the mobile client only.**

**In scope (this phase only):**
- New `LoginScreen.tsx` with three CTAs (Continue with Apple, Continue with Google, Continue as Guest) — reached on demand from AccountScreen, NOT mandatory at app launch.
- New services: `appleSignIn.ts` (wraps `@invertase/react-native-apple-authentication`), `googleSignIn.ts` (wraps `@react-native-google-signin/google-signin`), `deepLink.ts` (handles `vpnapp://payment/success?invoiceId=X`).
- Rewrite `PaymentScreen.tsx`: remove existing 3-plan hardcoded cards + Telegram-CTA flow; replace with current-plan limits card + Pro features list (no prices) + single CTA "Upgrade to Pro at risevpn.com" + "Already paid? Refresh" link below.
- Interstitial "You're leaving the app" sheet between Upgrade tap and `Linking.openURL`.
- Rewrite `app/src/services/payment.ts`: drop `createCheckoutSession` (Stripe), drop `cancelSubscription` (deferred); add helpers for opening the upgrade URL with locale + invoice-polling helper that mirrors Phase 4 D-21 contract.
- `AccountScreen.tsx`: add "Sign in to sync Pro" section visible to guest users with Apple + Google buttons.
- Update `authStore.ts`: add `signInWithApple()`, `signInWithGoogle()` actions; preserve guest-promotion path (existing guest JWT in Authorization header signals backend D-06 in-place promotion).
- iOS: enable "Sign in with Apple" capability in `app/ios/VpnApp.xcodeproj`, add `CFBundleURLTypes` for `vpnapp` scheme in `Info.plist`, add Google's reversed-client-id URL scheme, wire `application(_:open:options:)` in `AppDelegate.swift` to forward to RN's `Linking`.
- Android: register `intent-filter` for `vpnapp` scheme on `MainActivity` in `AndroidManifest.xml`; register Google OAuth Android client (signed with debug keystore SHA-1 for local testing).
- Pro-return handshake: deep-link receive → modal overlay "Activating Pro…" → poll `GET /invoices/{id}` every 2s, add `?escalate=true` from poll #6 (10s onward), 30s total timeout matching Phase 4 D-21 cadence. On `status=paid` close modal + refresh `/account`. On 30s timeout show "Processing… we'll email you" + Telegram support (`https://t.me/flawlssr`).
- Foreground safety-net: extend `HomeScreen.tsx`'s existing `AppState.active` hook (lines 41–50) so it refreshes subscription state in addition to `/account`, catching users who paid on web but closed the browser before tapping "Open in app".
- Version bump: `app/package.json` (2.1.0→2.2.0), `app/src/config/version.ts` `APP_VERSION`, `app/android/app/build.gradle` `versionName` + `versionCode` (e.g. 21→22), `app/ios/VpnApp.xcodeproj/project.pbxproj` `MARKETING_VERSION` + `CURRENT_PROJECT_VERSION`.
- Local Android release build (`./gradlew bundleRelease` or `npm run android:release`) produces a signed `.aab`; operator smoke-tests on their Android device.
- i18n keys added to `app/src/i18n/en.json` + `app/src/i18n/ru.json` for: `login.*`, `payment.upgrade.*`, `payment.activating.*`, `payment.takingLonger.*`, `account.signInToSync.*`, plus the "You're leaving the app" sheet copy.

**Out of scope (deferred to other phases or explicitly descoped):**
- TestFlight upload (iOS) and Play Internal Track upload (Android) — operator explicit choice: descoped from this phase, will be handled in an end-of-milestone release phase after all functional work is done. APP-07's "build ships to TestFlight + Play Internal" success-criterion wording is **deliberately deviated from**: the bar for Phase 5 is "build produced + locally smoke-tested on Android", not "uploaded".
- fastlane / CI release automation — deferred to the same end-of-milestone phase.
- iOS smoke-test on a physical iPhone — operator does not have iOS hardware/Apple Connect set up. iOS code lands and is type-checked, but the smoke-test bar is Android-only this phase.
- Universal Links (`https://risevpn.com/pay/success` → app via `apple-app-site-association` + `assetlinks.json`) — ROADMAP and ADR §12.4 both lock the custom-scheme `vpnapp://`. Universal Links remain a future hardening.
- Token storage migration from `AsyncStorage` to `MMKV` — ADR §12.6 says no change; revisit when MMKV adoption grows elsewhere.
- Mobile consumption of the new JWT `plan_id` claim from Phase 3 D-29 — Phase 4 D-17 uses it on web; on mobile we keep reading the legacy `subscription_tier` field from `/account` which backend continues to populate per Phase 4 D-17 ("denormalised copy stays"). Revisit when the plan name needs to show on mobile.
- ES (Spanish) locale on mobile — mobile carries RU + EN; landing carries RU + EN + ES. Mobile→pricing redirect maps EN-or-other → `/en/pricing`, RU → `/ru/pricing`. ES locale addition to mobile is a separate later phase.
- "Merge accounts" UI for users with separate Apple-on-web and Google-on-mobile identities — ADR §13 row 7, Phase 6 work.
- Share-code Pro warning before SSO sign-in — ADR §13 row 9 edge case. Deferred.
- `cancelSubscription` button on mobile — backend exists from Phase 3 03-05, UI deferred to Phase 7.
- "Restore purchase" via Apple StoreKit-style entitlement check — N/A, no IAP. The "Already paid? Refresh" link is the mobile-side restore path.

</domain>

<decisions>
## Implementation Decisions

> Every entry is a **locked decision** sourced from the /gsd-discuss-phase session unless marked **Claude's Discretion**. These supersede defaults in ROADMAP, ADR-007, or RESEARCH guesses.

### Login gating

- **D-01:** **Auto-guest on app launch is preserved.** `authStore.initialize()` keeps its existing behavior: read stored tokens, and if absent, call `POST /auth/guest` with device fingerprint. **LoginScreen is NOT a mandatory first screen.** It is reached on demand from `AccountScreen` ("Sign in to sync Pro" section). **Deliberate divergence from ROADMAP success criterion #1's literal wording** ("LoginScreen is the auth entry for users without a session"). SC#1's tester path still works: tester navigates Account → Sign in → LoginScreen → Continue with Apple → Home. The test command in 05-HUMAN-UAT.md must reflect this navigation step.
- **D-02:** **`LoginScreen.tsx` is a navigable destination, not a route-gate.** Implemented as a normal `Stack.Screen` in `RootNavigator.tsx` under the name `Login`. Reached by `navigation.navigate('Login')` from AccountScreen's "Sign in to sync Pro" button. Three CTAs vertically stacked: "Continue with Apple" (iOS only — hidden on Android), "Continue with Google" (both platforms), "Continue as Guest" (cancels navigation and returns to AccountScreen if user is already a guest; otherwise calls `/auth/guest` and navigates Home). Phase 5 ships the screen but the existing auto-guest path means most users never see it on first launch.
- **D-03:** **AccountScreen gains a "Sign in to sync Pro" card** visible only when `auth_provider === 'guest'` (or `user.auth_provider` is undefined for v2.1.0 carry-over). Card content: brief copy ("Sign in with Apple or Google to keep your Pro across devices") + two side-by-side buttons (Apple, Google) that route to the `LoginScreen` with the corresponding provider pre-selected (or directly invoke the provider sheet — planner's call between same screen vs direct provider invoke). Once user signs in, `auth_provider` flips to `apple`/`google` and the card disappears.
- **D-04:** **Account-linking by verified email is silent.** When a user already signed in with Apple on device-A signs in with Google on device-B with the same verified email (and not `@privaterelay.appleid.com`), backend Phase 2 D-03 auto-links them. Mobile shows **no special UI** — user lands on Home with their existing Pro plan visible. If the Apple email is private-relay, backend D-04 skips auto-link and creates a separate `users.id`; the user discovers this only by missing Pro state on the second device. **Claude's Discretion**: surfacing linked-provider chips on AccountScreen is deferred.
- **D-05:** **Silent transition to Home on guest → SSO success.** No splash, no toast. `authStore` replaces the guest tokens with the new SSO-issued tokens (same `users.id` preserved by backend D-06 in-place promotion); RN navigation pops the login flow and the existing `RootStackParamList` Home screen renders. Mirrors Apple/Google native UX expectations.
- **D-06:** **Guest JWT MUST be sent in `Authorization: Bearer` header** to `/auth/apple` and `/auth/google` so the backend takes the in-place promotion path. `authStore.signInWithApple()` and `signInWithGoogle()` read the current guest token from state before initiating the provider sheet, then attach the header to the backend call. Same access-token attachment as today's axios interceptor — no new mechanism, just preserved on the SSO endpoints which today don't require auth.

### Pro-return handshake

- **D-07:** **Modal overlay on deep-link receive.** When `vpnapp://payment/success?invoiceId=X` fires and the app foregrounds (cold start or warm), `deepLink.ts` extracts `invoiceId`, presents a full-screen RN `Modal` (or `Stack.Screen` modal-presentation) showing a spinner + i18n copy `payment.activating.title` ("Activating your Pro subscription…"). The modal blocks dismissal while polling runs. Matches Phase 4 D-22 landing copy verbatim for cross-surface UX consistency.
- **D-08:** **Polling cadence = Phase 4 D-21 verbatim.** `GET /api/v1/invoices/{id}` every 2 seconds. Polls 1–5 (0–10s elapsed): no query string. Poll 6 onward (10s+): append `?escalate=true` to force the backend → lava fallback. Total timeout: 30 seconds. On `status === 'paid'` → close modal, call `authStore.fetchAccount()`, show toast "Pro is active!" on Home for ~3 seconds. On `status === 'failed'` → close modal, navigate to PaymentScreen with error state.
- **D-09:** **Foreground safety-net extends existing HomeScreen hook.** `app/src/screens/HomeScreen.tsx` lines 41–50 already calls `fetchAccount()` on `AppState.active`. Phase 5 extends this so it also picks up subscription state — either by ensuring `/account` response includes the updated `subscription_tier` (already does per Phase 2 contract) or by adding a secondary call to a `/subscription` endpoint if needed (planner verifies which is canonical). This is the safety net for users who paid on web but closed the browser before tapping "Open in app" — they get Pro on next foreground without any deep-link trigger.
- **D-10:** **30s timeout copy matches Phase 4 D-22.** When polling exhausts without `status === 'paid'`, modal stops polling and switches to a static state: i18n key `payment.takingLonger.title` ("We're processing your payment — we'll email you when it's active") + a "Refresh" button (re-fires `fetchAccount` + re-attempts a single `GET /invoices/{id}`) + a Telegram support link (`https://t.me/flawlssr` — already used in `app/src/services/payment.ts` line 25). Modal stays open until user dismisses or hits Refresh. **No auto-retry from this state** — user has agency.
- **D-11:** **Modal scope is global, not PaymentScreen-local.** The deep-link can fire from any screen (Home, Server List, even Settings) because it comes from outside the app. The polling modal must render above whatever screen is active. Implement via `RootNavigator` modal-presentation screen OR a context-provided overlay component at `App.tsx` root (planner picks; both work in RN). State lives in a new `paymentReturnStore` (or extends `authStore` — planner's call).

### App Store compliance

- **D-12:** **Full-screen "You're leaving the app" interstitial sheet** between PaymentScreen Upgrade tap and `Linking.openURL`. Sheet content (i18n keys `payment.upgrade.leaving.*`):
  - Title: "You're leaving the app"
  - Body: "You'll continue to risevpn.com in your browser to upgrade. Pro will activate on this device automatically once payment is complete."
  - Primary button: "Continue" → invokes `Linking.openURL('https://risevpn.com/<locale>/pricing?return=app')`
  - Secondary button: "Cancel" → dismisses sheet
  Rationale: closer to Apple's external-link entitlement guidance (ADR §13 row 1); reduces App Store rejection risk. Reviewer-protection above conversion. Render as a native bottom sheet OR full-screen modal — planner picks based on RN modal ergonomics (no extra dep — use `Modal` from `react-native`).
- **D-13:** **CTA button text is exactly "Upgrade to Pro at risevpn.com"** (i18n key `payment.upgrade.cta`). Matches ROADMAP SC#3 verbatim. **No price displayed anywhere on the button** or on a CTA-styled element above/around it (SC#3 hard requirement). No "from $4.99/mo" subtext. No "Pro $4.99" anywhere on PaymentScreen.
- **D-14:** **PaymentScreen content structure** (top to bottom):
  1. **Current plan card.** Reads `user.subscription_tier` from authStore. For free users: card title "Your current plan: Free" + limits list ("1 device", "10 GB / month", "5 server locations"). For Pro users: title "Your current plan: Pro" + "Manage subscription" link → opens lava-hosted management URL (via `GET /api/v1/subscription/manage-url` — same endpoint Phase 4 D-16 defined; if missing, the link is hidden). **No prices shown** in either case.
  2. **"Pro includes" feature list** (free users only). Bullet items, no prices: "Unlimited speed", "All servers worldwide", "Up to 5 devices", "No ads". Pulls from i18n; copy locked here.
  3. **Single CTA** (free users only): "Upgrade to Pro at risevpn.com" — opens interstitial sheet per D-12.
  4. **Tertiary "Already paid? Refresh" link** beneath CTA. Small text-style affordance (not a button). Triggers `authStore.fetchAccount()` + a single `GET /invoices/{id}` if the user has a recent `pending` invoice (planner: this requires knowing the invoice id; simplest is to just `fetchAccount()` and call it good; deeper logic out of scope).
  5. **Removed entirely:** the existing Telegram-CTA flow (`openTelegram` function), the `SUPPORT_TELEGRAM` constant in PaymentScreen, the 3-plan hardcoded cards. `app/src/services/payment.ts` no longer exports `createCheckoutSession` or `cancelSubscription` (Stripe removal is a Phase 8 task but the Phase 5 file rewrite breaks the imports anyway — coordinate with Phase 8 planner via the canonical refs).
- **D-15:** **Restore-purchase affordance is non-prominent.** Visible but tertiary-weight — small text link below the main CTA, not a same-prominence button. Avoids "looks like an alternative buy path" reviewer reading. Tap calls `authStore.fetchAccount()` and shows a transient toast ("Refreshed" or "No change").
- **D-16:** **Locale for the upgrade URL** is derived from current i18next locale: `ru` → `https://risevpn.com/ru/pricing?return=app`, otherwise → `https://risevpn.com/en/pricing?return=app`. Mobile does not have ES, so an EN fallback is correct. The `?return=app` query parameter signals the landing's `/pricing` page that this user came from the mobile app — landing's auto-checkout-on-return flow (Phase 4 D-19) reads this. If `?return=app` is not currently consumed by landing, that's a Phase 4 follow-up to wire (or landing already accepts it; planner verifies).

### Build, release, & versioning

- **D-17:** **All four version sources updated in this phase:**
  - `app/package.json` → `"version": "2.2.0"`
  - `app/src/config/version.ts` → `APP_VERSION = '2.2.0'` (sent as `X-App-Version` request header per `app/src/services/api.ts` L19)
  - `app/android/app/build.gradle` → `versionName "2.2.0"` + `versionCode` incremented (current is implied 21 or similar — planner reads and bumps by 1)
  - `app/ios/VpnApp.xcodeproj/project.pbxproj` → `MARKETING_VERSION = 2.2.0` + `CURRENT_PROJECT_VERSION` incremented
  ROADMAP wording "app.json reads 2.2.0" is colloquial — `app/app.json` (`{name, displayName}` only) is left alone. SC#5 verified by reading the four files above.
- **D-18:** **No fastlane / no CI upload / no TestFlight + no Play Internal upload in this phase.** **Explicit scope deviation from APP-07.** Operator's choice: uploads happen in a later end-of-milestone release phase after all milestone work is done and ready to ship together. Phase 5's release bar is "code complete + signed local Android build + smoke-tested on operator's physical Android device". The new end-of-milestone release phase will be added to the roadmap (planner does NOT modify ROADMAP here — that's a separate `/gsd-add-phase` task).
- **D-19:** **Local Android build verification** is the SC#5 evidence path:
  - Run `cd app && npm run android` (debug) on operator's device for development smoke (Apple sign-in won't work on Android; Google sign-in must work).
  - Run a release-config build: `cd app/android && ./gradlew bundleRelease` to produce a signed `.aab`. Use a release keystore (operator must have one; if not, generate a phase-internal one and document its location — the production keystore lifecycle moves to the end-of-milestone release phase).
  - `adb install` the resulting APK (extracted from the AAB via `bundletool` or built separately via `assembleRelease`) on the operator's device and verify: (a) Google sign-in flow works, (b) Guest sign-in path works (preserves users.id when upgrading from guest), (c) PaymentScreen renders informational layout with no buy button + no prices, (d) Tapping CTA opens interstitial → Linking opens browser to risevpn.com, (e) Deep-link receive: from a browser on the device, type `vpnapp://payment/success?invoiceId=test123` → app opens to polling modal.
- **D-20:** **iOS code lands but iOS smoke-test is deferred.** All iOS files (Info.plist `CFBundleURLTypes`, entitlements file, AppDelegate URL handler) MUST land and compile cleanly, but the operator does not have iOS hardware + Apple Developer credentials set up to run a build. Verification of iOS happens in the end-of-milestone release phase. The phase's `must_haves` enumerate iOS file presence but NOT iOS runtime behavior.
- **D-21:** **Operator prerequisites checklist gates the phase** as a `[BLOCKING]` task in the plan. Narrowed scope (no upload → no production keystore / Apple Connect API / Play Console service account needed yet):
  - **Required this phase:**
    - Apple Service ID + Bundle ID with "Sign in with Apple" capability enabled (already needed for Phase 2 — verify operator has these)
    - Google OAuth client IDs for iOS + Android + Web (Phase 2 already needs Web — Phase 5 adds iOS + Android)
    - Android debug keystore SHA-1 registered with the Android Google OAuth client (so Google sign-in works during local smoke-test)
  - **NOT required this phase** (deferred to end-of-milestone release phase):
    - Apple App Store Connect API key (`.p8` + key ID + issuer)
    - Play Console service account JSON
    - Production Android release keystore (a phase-internal release keystore is acceptable; replace later)
    - Apple external-link entitlement form approval (operational, parallel track)
  - The plan's `[BLOCKING]` task includes a checklist the operator must confirm before code execution starts. If any required-this-phase item is missing, the plan stops at the operator-prereqs gate and reports which item is missing.

### Threat model (operator-facing — for planner's <threat_model> blocks)

- **D-22:** **`security_enforcement` gate is enabled** (`.planning/config.json` default true) — every PLAN.md MUST include a `<threat_model>` block. At minimum each PLAN's threat model must cover:
  - **T-1: Deep-link spoofing.** A malicious web page or app fires `vpnapp://payment/success?invoiceId=ATTACKER_VALUE` to trick the user into thinking they have Pro. Mitigation: the polling step verifies status against the backend (`/invoices/{id}` returns `paid` only if the real lava webhook delivered the success event). Backend authorization on `/invoices/{id}` must scope to current user — verify Phase 3 03-05 contract enforces this.
  - **T-2: ID-token replay.** A captured Apple `identityToken` or Google `idToken` is replayed against our backend. Mitigation: backend Phase 2 D-16 already validates signature + iss + aud + exp. Mobile contribution: don't log tokens, don't persist them in MMKV/AsyncStorage, treat as transient.
  - **T-3: Apple authorization-code leakage.** Apple returns an `authorizationCode` alongside `identityToken`; backend Phase 2 D-18 deferred server-side exchange. Mobile MAY pass the code to backend, MUST NOT persist it locally.
  - **T-4: Universal-clipboard / browser-history leak of `invoiceId`.** The user's browser history shows `https://risevpn.com/pay/success?invoiceId=X` after they tap "Open in app". Mitigation: invoiceId is per-user and non-sensitive by itself (it identifies an invoice but doesn't grant access without an auth token). Document the leak surface; no additional mitigation needed at v1.
  - **T-5: Token storage on a rooted/jailbroken device.** `AsyncStorage` is plain on Android, Keychain-ish on iOS. Mitigation: existing risk preserved from v2.1.0; ADR §12.6 says no change this phase. Document as known.
  - **T-6: SSO library supply-chain risk.** `@invertase/react-native-apple-authentication` + `@react-native-google-signin/google-signin` are large new native deps. Mitigation: pin exact versions, document SHA in lockfile, run `npm audit` as part of plan task.
  - **T-7: 401 → refresh → retry feedback loop on SSO failure.** Existing axios interceptor (`api.ts` L51-115) on a `/auth/apple` 401 attempts refresh — but `/auth/apple` is unauthenticated and shouldn't 401 on missing JWT. Verify interceptor short-circuits for `/auth/*` endpoints OR add a `_skipAuthRefresh` flag to the SSO calls.

### Claude's Discretion

- **Sheet vs full-screen Modal** for the "You're leaving the app" interstitial — D-12 locks the content but not the visual form factor. Default: full-screen RN `Modal` for visual weight; planner may choose a bottom sheet if a sheet library is already available without adding a dep.
- **`paymentReturnStore` vs extending `authStore`** for the polling/modal state — D-11. Default: extend `authStore` with `pendingInvoiceId` + `isActivatingPro` fields; planner may split if it gets unwieldy.
- **i18n key namespace.** Phase 5 adds `login.*`, `payment.upgrade.*`, `payment.activating.*`, `payment.takingLonger.*`, `account.signInToSync.*`. Planner picks exact key names; this is the namespace.
- **AccountScreen "Sign in to sync Pro" card visual treatment.** D-03 locks the existence and trigger condition; planner picks card styling consistent with existing AccountScreen patterns.
- **`useSubscription` hook re-wiring after PaymentScreen rewrite.** Existing hook is used elsewhere (HomeScreen indirectly). Planner verifies the hook is still consumed and either keeps it or replaces with direct `authStore.user.subscription_tier` reads where appropriate. Refactor scope is bounded to PaymentScreen.
- **Whether to add `_skipAuthRefresh` flag** in `app/src/services/api.ts` for SSO endpoints (T-7 mitigation) vs adding a URL-pattern check in the existing 401 interceptor. Planner picks; T-7 must be addressed one way or another.
- **Pre-selected provider on LoginScreen from AccountScreen "Sign in to sync Pro" buttons** (D-03). Default: AccountScreen routes `navigation.navigate('Login')` with no params and LoginScreen shows all three CTAs; alternative: pass `{provider: 'apple'|'google'}` and LoginScreen auto-invokes the sheet. Planner picks based on UX simulation.

### Folded Todos

None — no pending todos matched Phase 5 from `gsd-tools todo match-phase 5`.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Architecture & Decisions
- `docs/ADR-007-lava-sso-rework.md` §4 — Component boundaries (mobile is `app/src/`)
- `docs/ADR-007-lava-sso-rework.md` §6.1, §6.2, §6.5 — Mobile auth flow diagrams (Apple, Google, cross-surface continuity)
- `docs/ADR-007-lava-sso-rework.md` §7 — JWT strategy (no claim-shape change for mobile)
- `docs/ADR-007-lava-sso-rework.md` §12 — Mobile app changes (entitlements, deps, UI structure)
- `docs/ADR-007-lava-sso-rework.md` §13 rows 1, 3, 7, 9, 10 — Risks (App Store, private relay, dual-provider link, share-code Pro, JWKs outage)
- `docs/ADR-007-lava-sso-rework.md` §15 open questions 1, 2, 7 — Apple/Google asset prep + RN module choice (confirms `@invertase` + `@react-native-google-signin`)

### Prior Phase Context (Carry-Forward Decisions — Backend + Web Contracts)
- `.planning/phases/02-auth-sso-backend/02-CONTEXT.md` D-06 — Guest → identified in-place promotion (preserves `users.id`); mobile MUST send guest JWT in `Authorization: Bearer` for this path to trigger
- `.planning/phases/02-auth-sso-backend/02-CONTEXT.md` D-19, D-20, D-21, D-22 — `/auth/apple` and `/auth/google` request + response shapes (mobile sends `identityToken` for Apple, `idToken` for Google, plus optional `deviceId` + `deviceSecret` + `platform: 'ios'` and the existing X-App-Version header)
- `.planning/phases/02-auth-sso-backend/02-CONTEXT.md` D-23, D-24 — `/auth/logout` contract (mobile uses this on sign-out from AccountScreen)
- `.planning/phases/03-lava-top-plans-catalog/03-05-checkout-cancel-invoices-admin-lava-proxy-SUMMARY.md` — `GET /api/v1/invoices/:id?escalate=true` contract that mobile polling reuses
- `.planning/phases/04-landing-surfaces/04-CONTEXT.md` D-19 — Logged-out auto-checkout-on-return-from-mobile flow (`?return=app` query parameter)
- `.planning/phases/04-landing-surfaces/04-CONTEXT.md` D-21 — Polling cadence (2s, escalate after 10s, 30s timeout) — mobile mirrors this verbatim
- `.planning/phases/04-landing-surfaces/04-CONTEXT.md` D-22 — `pay.success.*` i18n copy keys that mobile mirrors

### Existing Mobile Codebase (Files Phase 5 Modifies or Extends)
- `app/src/stores/authStore.ts` — existing zustand auth store; Phase 5 adds `signInWithApple()` + `signInWithGoogle()` actions, optionally a `pendingInvoiceId` field
- `app/src/services/api.ts` — existing axios client with 401→refresh→retry; T-7 in threat model: ensure SSO endpoints don't recurse the refresh loop
- `app/src/services/payment.ts` — existing Stripe-era helpers; Phase 5 deletes `createCheckoutSession` + `cancelSubscription`, adds upgrade-URL helper + invoice-polling helper
- `app/src/services/deviceFingerprint.ts` — existing device-fingerprint generator; reused by guest path on LoginScreen
- `app/src/screens/PaymentScreen.tsx` — full rewrite per D-14; today has hardcoded 3-plan cards + Telegram CTA flow
- `app/src/screens/AccountScreen.tsx` — adds "Sign in to sync Pro" card per D-03
- `app/src/screens/HomeScreen.tsx` lines 41–50 — extend existing `AppState.active` hook for D-09 foreground safety net
- `app/src/navigation/RootNavigator.tsx` — add `Login` Stack.Screen per D-02
- `app/src/types/api.ts` — extend `User` type with `auth_provider`, `email`, `email_verified` (matches Phase 2 D-11 GORM columns)
- `app/src/i18n/en.json` + `app/src/i18n/ru.json` — new key namespaces per D-13, D-14
- `app/src/config/version.ts` — bump per D-17
- `app/package.json` — bump per D-17

### Native Files (Phase 5 Creates or Modifies)
- `app/ios/VpnApp/Info.plist` — add `CFBundleURLTypes` for `vpnapp` scheme + Google reversed-client-id (Google sign-in iOS requirement)
- `app/ios/VpnApp/AppDelegate.swift` — wire `application(_:open:options:)` to RN's `RCTLinkingManager`
- `app/ios/VpnApp/VpnApp.entitlements` — add `Sign in with Apple` capability entry
- `app/ios/VpnApp.xcodeproj/project.pbxproj` — `MARKETING_VERSION` + `CURRENT_PROJECT_VERSION` bumps; embed Google sign-in pod
- `app/ios/Podfile` — add `RNAppleAuthentication` + `GoogleSignIn` pods (auto-linked by RN community CLI for most cases, but verify)
- `app/android/app/src/main/AndroidManifest.xml` — add `<intent-filter>` for `vpnapp://payment/success` on MainActivity
- `app/android/app/build.gradle` — `versionName` + `versionCode` bumps; ensure `googleSignInClientId` build config or string resource registered
- `app/android/app/src/main/res/values/strings.xml` (or new) — Google sign-in default client ID resource

### New Mobile Services (Phase 5 Adds)
- `app/src/services/appleSignIn.ts` (new) — wraps `@invertase/react-native-apple-authentication`; exposes `signInWithApple(): Promise<{identityToken, authorizationCode, fullName?, email?}>`
- `app/src/services/googleSignIn.ts` (new) — wraps `@react-native-google-signin/google-signin`; exposes `signInWithGoogle(): Promise<{idToken}>` plus `configure()` called at app boot
- `app/src/services/deepLink.ts` (new) — `Linking.addEventListener('url')` registration + URL parser for `vpnapp://payment/success?invoiceId=X`; dispatches to authStore action

### Backend Contracts (Already Shipped — Mobile Consumes)
- `server/api/internal/handler/auth.go` — `/auth/apple`, `/auth/google`, `/auth/guest`, `/auth/refresh`, `/auth/logout`
- `server/api/internal/handler/payment.go` — `GET /invoices/:id?escalate=true`
- `server/api/internal/handler/account.go` (or similar) — `GET /account` returning `subscription_tier`, `auth_provider`, `email`
- `docs/lava-payments-api.md` — operator-facing API reference

### Requirements
- `.planning/REQUIREMENTS.md` §App — APP-01 through APP-07 (mobile-side acceptance criteria)
- `.planning/ROADMAP.md` §"Phase 5: Mobile SSO + Pro CTA" — phase goal + 5 numbered success criteria (note SC#1 path deviation per D-01, SC#5 verification scope per D-18/D-19)

### Project-Wide Rules
- `CLAUDE.md` (project root) — GSD workflow enforcement, lava-only / Apple+Google-only / no-IAP constraints, mobile tech stack lock (RN 0.84 + Zustand + axios)
- `~/.claude/CLAUDE.md` (user globals) — architect-first + quality pipeline pattern; treat both as planning input

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`authStore` (`app/src/stores/authStore.ts`)** — fully wired zustand store with token persistence (AsyncStorage), guest auto-login concurrency guard, refresh-token rotation. Phase 5 adds two new actions (`signInWithApple`, `signInWithGoogle`) following the existing `linkWithCode` pattern (line 80) — same shape: provider-specific request → set tokens + isAuthenticated → fetchAccount.
- **`api.ts` axios client** — already does 401→refresh→retry with a global refresh lock (lines 33–115). Phase 5 inherits this for the `/account` calls inside the polling loop. **Must verify** that SSO endpoint calls don't recurse the refresh loop (threat model T-7).
- **`getDeviceFingerprint()`** — already used by `/auth/guest`. Apple and Google sign-in handlers optionally accept `deviceId` + `deviceSecret` (Phase 2 D-20, D-22) — reuse the same helper to bind the device row on first SSO sign-in.
- **`useSubscription` hook** — current PaymentScreen uses it; Phase 5 keeps it for reading `subscription_tier` but the consumer surface shrinks (no more plan-cards iteration).
- **`Linking.openURL`** — already used in PaymentScreen line 163 for the Telegram CTA. Phase 5 reuses the same RN API for the risevpn.com/pricing redirect.
- **`useTranslation` (i18next)** — already pervasive. New keys go in `en.json` + `ru.json` only; ES is landing-only.
- **`AppState.addEventListener('change')`** — already used in HomeScreen lines 43–48 for the foreground hook. Phase 5 just extends it.

### Established Patterns
- **Server-component absent, all screens are functional React components with hooks** — standard RN.
- **Theme tokens centralized** at `app/src/theme/` (colors, typography, spacing, borderRadius) — Phase 5 reuses; no new tokens.
- **Screen structure**: `SafeAreaView > ScrollView > content`. PaymentScreen rewrite follows the same shell.
- **Error handling**: `Alert.alert()` for blocking errors, silent failure with comment for non-critical. Phase 5 matches.
- **Navigation params via `useNavigation<NativeStackNavigationProp<RootStackParamList>>()`** typed pattern. Phase 5 adds `Login` to `RootStackParamList`.
- **i18n keys are deeply nested** (e.g., `payment.plans.premium.name`). Phase 5 namespace follows: `payment.upgrade.cta`, `payment.activating.title`, etc.

### Integration Points
- **`RootNavigator.tsx`** is the single source of truth for screens — adding `LoginScreen` requires one entry in the `RootStackParamList` type + one `<Stack.Screen>` element.
- **`App.tsx`** is the boot location for `Linking.addEventListener` — Phase 5 wires `deepLink.ts` here (or via a hook in `RootNavigator`).
- **Google sign-in `configure()`** must run at app boot, BEFORE the first sign-in attempt — wire in `App.tsx` next to existing init.
- **iOS `AppDelegate.swift`** is the place for `application(_:open:options:)` — RN community provides `RCTLinkingManager.application:openURL:options:` as the standard forwarder.
- **Phase 8 cleanup conflict warning**: Phase 8 (HARD-01) deletes Stripe from `handler/payment.go` etc. Phase 5 deletes the mobile-side Stripe consumer (`createCheckoutSession` in `payment.ts`). Phase 5 doing this is OK because there are no paying users — but Phase 8 planner must not assume mobile still imports it. Coordinate via canonical refs.

### Architectural Constraints
- **RN bare workflow (not Expo)** — every native dep needs to be auto-linked or manually wired. Both `@invertase/react-native-apple-authentication` and `@react-native-google-signin/google-signin` support auto-linking on RN 0.84.
- **Token storage = AsyncStorage** today (per D-CD MMKV deferred). Don't introduce a new persistence layer in Phase 5.
- **No new third-party state libs** — zustand stays; no Redux, no Recoil.
- **`X-App-Version: 2.2.0` header** must be sent on every request after the bump — already automatic via `api.ts` once `APP_VERSION` is bumped.
- **Server `MIN_APP_VERSION` env** — operator must ensure the backend doesn't reject v2.2.0 as below-min. Coordinate via 05-HUMAN-UAT.md.

</code_context>

<specifics>
## Specific Ideas

- **Deep link URL exactly**: `vpnapp://payment/success?invoiceId=X` — locked by ROADMAP SC#4 and ADR §12.4. No variations.
- **CTA copy exactly**: `Upgrade to Pro at risevpn.com` — locked by ROADMAP SC#3 and D-13. No variations.
- **Polling cadence exactly**: 2s interval, append `?escalate=true` from poll #6 onward, 30s timeout — locked by D-08 mirroring Phase 4 D-21.
- **Timeout copy exactly mirrors Phase 4** i18n key `payment.takingLonger.title` ("We're processing your payment — we'll email you when it's active") + Telegram link `https://t.me/flawlssr`.
- **Auto-link is silent on success** (D-04). No UI feedback for the cross-provider link case.
- **Modal blocks dismissal during polling** (D-07). User cannot tap-outside to close while activating.
- **CTA opens an interstitial sheet, not Linking.openURL directly** (D-12). One extra tap. App Store reviewer protection.
- **Locale derivation for upgrade URL**: i18next current locale → `ru` or `en`; never ES on mobile.
- **All four version files bumped together** (D-17) — not just `package.json`.
- **NO upload to TestFlight or Play Internal this phase** (D-18). Local Android smoke is the bar.

</specifics>

<deferred>
## Deferred Ideas

- **TestFlight upload (iOS) + Play Internal Track upload (Android)** → end-of-milestone release phase (to be added to ROADMAP via separate `/gsd-add-phase`)
- **fastlane / CI release automation** → same end-of-milestone release phase
- **iOS smoke-test on physical iPhone** → end-of-milestone release phase (operator iOS hardware not set up yet)
- **Universal Links** (`https://risevpn.com/pay/success` opens app if installed) — current spec uses custom scheme `vpnapp://`
- **MMKV token storage migration** — ADR §12.6 says no change; revisit when MMKV adoption grows
- **Mobile consumption of `plan_id` JWT claim** — backend keeps populating `subscription_tier` per Phase 4 D-17
- **ES locale on mobile** — landing has it; mobile doesn't. Separate later phase
- **"Merge accounts" UI** for cross-provider distinct-email users — ADR §13 row 7, Phase 6
- **Share-code Pro warning before SSO** — ADR §13 row 9 edge case
- **`cancelSubscription` UI on mobile** — backend exists from Phase 3 03-05; UI deferred to Phase 7 admin overhaul (or never if operator decides "manage subscription opens lava portal" is enough)
- **Apple external-link entitlement form submission** — operational paperwork, parallel track; submission helpful but not blocking for Phase 5 local-only verification
- **"Restore purchase" with full invoice history fetch** — current "Already paid? Refresh" affordance just fetches `/account`; deeper history view is deferred
- **Linked-provider chips on AccountScreen** — D-04 left silent; if support tickets show users confused about "which provider am I signed in with", revisit
- **Splash / welcome animation on SSO success** — D-05 silent transition; revisit if user research shows users miss confirmation
- **Per-provider error toasts** (e.g., "Apple Sign-In was cancelled") — Claude's Discretion in planning; default is silent return-to-LoginScreen

### Reviewed Todos (not folded)
None — no pending todos matched Phase 5.

</deferred>

---

*Phase: 05-mobile-sso-pro-cta*
*Context gathered: 2026-05-26 via /gsd-discuss-phase interactive session*
