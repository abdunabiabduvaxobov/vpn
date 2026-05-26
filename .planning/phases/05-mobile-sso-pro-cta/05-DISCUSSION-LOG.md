# Phase 5: Mobile SSO + Pro CTA - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in `05-CONTEXT.md` — this log preserves the alternatives considered.

**Date:** 2026-05-26
**Phase:** 05-mobile-sso-pro-cta
**Areas discussed:** Login gating model, Pro-return handshake, App Store compliance posture, Build & release scope

---

## Area Selection (multiSelect)

**Question:** Which gray areas do you want to discuss for Phase 5?

| Option | Description | Selected |
|--------|-------------|----------|
| Login gating model (Recommended) | Mandatory LoginScreen vs auto-guest with optional sign-in | ✓ |
| Pro-return handshake (Recommended) | Deep-link receive → polling → Home transition + Restore button | ✓ |
| App Store compliance posture (Recommended) | Interstitial vs direct CTA, copy choice, Restore-button prominence | ✓ |
| Build & release scope (TestFlight + Play Internal) | fastlane vs manual upload, version-bump targets, verification bar | ✓ |

**User's choice:** All four areas.

---

## Login gating model

### Q1: When the app launches with no stored session, what should the user see first?

| Option | Description | Selected |
|--------|-------------|----------|
| LoginScreen first, no auto-guest (Recommended) | Mandatory entry per ROADMAP SC#1 literal wording | |
| Auto-guest, Login accessible from AccountScreen | Preserves today's friction-free first launch; Login on demand | ✓ |
| Hybrid: auto-guest BUT show one-time intro | First launch shows intro modal with Apple/Google + Skip | |

**User's choice:** Auto-guest, Login accessible from AccountScreen
**Notes:** Deliberate divergence from SC#1 literal wording. Tester path still works through AccountScreen → Sign in → LoginScreen → Apple → Home. The SC#1 wording will be considered satisfied by this navigation path.

### Q2: How does a v2.1.0 guest user upgrading to v2.2.0 reach SSO?

| Option | Description | Selected |
|--------|-------------|----------|
| 'Sign in to sync Pro' button on AccountScreen (Recommended) | Most discoverable card in AccountScreen | ✓ |
| Force LoginScreen on next launch after update | Clear guest tokens → route all users to Login once | |
| Subtle banner on HomeScreen | Yellow/blue strip with dismissal state | |

**User's choice:** 'Sign in to sync Pro' button on AccountScreen (Recommended)

### Q3: Cross-provider same-email sign-in (auto-link) UX

| Option | Description | Selected |
|--------|-------------|----------|
| Just sign in — backend handles auto-link silently (Recommended) | No mobile UI for the link case | ✓ |
| Show 'Linked accounts' toast after sign-in | Disclosure toast | |
| Show linked providers card on AccountScreen | Permanent disclosure card | |

**User's choice:** Just sign in — backend handles auto-link silently (Recommended)

### Q4: Guest → SSO success transition UX

| Option | Description | Selected |
|--------|-------------|----------|
| Silent transition straight to Home (Recommended) | No splash, no toast | ✓ |
| Quick 'Welcome, {name}!' splash for 2 seconds | Confirmation overlay | |
| Toast on Home: 'Signed in with Apple as {email}' | Non-blocking confirmation | |

**User's choice:** Silent transition straight to Home (Recommended)

---

## Pro-return handshake

### Q1: UI on deep-link foreground

| Option | Description | Selected |
|--------|-------------|----------|
| Modal overlay 'Activating Pro…' with spinner (Recommended) | Blocking modal mirroring Phase 4 D-22 | ✓ |
| Dedicated 'Payment Success' screen pushed onto stack | Full-screen with navigation context | |
| Silent: just fetch /account once, no UI | No modal, optimistic | |

**User's choice:** Modal overlay 'Activating Pro…' with spinner (Recommended)

### Q2: Polling cadence

| Option | Description | Selected |
|--------|-------------|----------|
| Match landing pattern: 2s interval, ?escalate=true after 10s, 30s timeout (Recommended) | Identical to Phase 4 D-21 | ✓ |
| Faster: 1s interval, escalate from second poll, 15s timeout | Tighter loop | |
| Lighter: single /account fetch first, fall back to polling | Optimistic | |

**User's choice:** Match landing pattern: 2s interval, ?escalate=true after 10s, 30s timeout (Recommended)

### Q3: Auto-refresh subscription on every app foreground (safety net)

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — HomeScreen already does this; extend to refresh subscription too (Recommended) | Reuse existing AppState.active hook | ✓ |
| Only on deep-link or explicit Refresh button | Minimize /account calls | |
| Yes, AND add 'Refresh subscription' button as belt-and-suspenders | Both auto-refresh AND visible button | |

**User's choice:** Yes — HomeScreen already does this; extend to refresh subscription too (Recommended)

### Q4: 30s poll timeout UX

| Option | Description | Selected |
|--------|-------------|----------|
| Match landing: 'Processing… we'll email you' + Telegram support (Recommended) | Mirrors Phase 4 D-22 copy verbatim | ✓ |
| Show error: 'Payment processing taking longer than expected' | Alarmist framing | |
| Dismiss silently to Home; user can hit 'Refresh subscription' manually | No error, no copy | |

**User's choice:** Match landing: 'Processing… we'll email you' + Telegram support (Recommended)

---

## App Store compliance posture

### Q1: Tap on Upgrade CTA → what does the user see?

| Option | Description | Selected |
|--------|-------------|----------|
| Full-screen interstitial sheet 'You're leaving the app' + Continue button (Recommended) | Aligned with Apple external-link entitlement guidance | ✓ |
| Direct Linking.openURL on tap, no interstitial | Fewer taps, higher reject risk | |
| Tiny inline disclaimer beneath button, no sheet | Compromise | |

**User's choice:** Full-screen interstitial sheet 'You're leaving the app' + Continue button (Recommended)

### Q2: Exact text on the PaymentScreen CTA button

| Option | Description | Selected |
|--------|-------------|----------|
| 'Upgrade to Pro at risevpn.com' (Recommended) | ROADMAP SC#3 verbatim | ✓ |
| 'Continue at risevpn.com' | ADR §12.4 wording | |
| 'Manage subscription on risevpn.com' | Maximum compliance, lowest conversion | |

**User's choice:** 'Upgrade to Pro at risevpn.com' (Recommended)

### Q3: What goes above the CTA on PaymentScreen?

| Option | Description | Selected |
|--------|-------------|----------|
| Current plan card + feature list (no prices) + CTA (Recommended) | Matches ADR §12.4 mockup | ✓ |
| Just the CTA + 'Pro features' bullet list | Minimal | |
| Feature comparison table (Free vs Pro side-by-side) + CTA | Two-column | |

**User's choice:** Current plan card + feature list (no prices) + CTA (Recommended)

### Q4: Restore-purchase affordance

| Option | Description | Selected |
|--------|-------------|----------|
| Small 'Already paid? Refresh' link below the main CTA (Recommended) | Tertiary-styled text link | ✓ |
| Hide — auto-refresh on foreground is enough | No visible button | |
| Equal-prominence secondary button: 'I already paid' | Same visual weight as Upgrade CTA | |

**User's choice:** Small 'Already paid? Refresh' link below the main CTA (Recommended)

---

## Build & release scope

### Q1: How are TestFlight + Play Internal builds produced for this phase?

| Option | Description | Selected |
|--------|-------------|----------|
| fastlane lanes in app/fastlane/ (Recommended) | In-phase code: automated lanes for both stores | |
| Manual: bump version, build signed artifacts, operator uploads via Xcode/Play Console | Phase ships version bumps only | |
| fastlane for Android only, manual for iOS | Asymmetric split | |
| **[Other — user free-text]** | "we will not upload yet, leave that out of phase and plan we will upload them in the end of all phases and testing on android building locally and testing on my phone" | ✓ |

**User's choice:** **DESCOPED from APP-07.** No upload to TestFlight or Play Internal in this phase. Local Android build + smoke-test on operator's physical Android device is the verification bar. iOS smoke-test deferred (no Apple Developer / iOS hardware set up yet). All upload work moves to a future end-of-milestone release phase.

**Notes:** This is a significant scope deviation from APP-07's literal wording ("build ships to TestFlight + Play Internal"). Recorded in CONTEXT.md D-18 with explicit "out of scope" framing. A future `/gsd-add-phase` task adds the end-of-milestone release phase that handles uploads.

### Q2: Operator prerequisites — what's required before phase can ship?

| Option | Description | Selected |
|--------|-------------|----------|
| Document deps explicitly, block phase on them (Recommended) | [BLOCKING] operator checklist gates plan | ✓ |
| Assume operator has them; document in CONTEXT.md only | No gate | |
| Phase ships a working build using sandbox/test OAuth client IDs | Phase-internal throwaway creds | |

**User's choice:** Document deps explicitly, block phase on them (Recommended)
**Notes:** Scope narrowed per Q1 — only the SSO-credential subset is required this phase (Google OAuth iOS+Android client IDs, Apple Service ID + Bundle ID, Android debug keystore SHA-1). Production keystore + Apple Connect API key + Play Console service account NOT required this phase.

### Q3: App version bump scope

| Option | Description | Selected |
|--------|-------------|----------|
| All four: package.json + version.ts + Android build.gradle + iOS project.pbxproj (Recommended) | Complete bump | ✓ |
| Just user-visible versions: skip versionCode/CURRENT_PROJECT_VERSION bumps | Risk store rejects | |
| Just app.json + version.ts — literal ROADMAP reading | Not viable | |

**User's choice:** All four: package.json + version.ts + Android build.gradle + iOS project.pbxproj (Recommended)

### Q4: Verification bar

| Option | Description | Selected |
|--------|-------------|----------|
| 'Build produced + smoke-tested locally' is enough (Recommended) | Local Android only | ✓ |
| 'Build uploaded' is the bar — phase blocks until TestFlight + Play show v2.2.0 | Strict reading of SC#5 | |
| Build produced + uploaded to internal-only Play Internal; iOS upload deferred | Asymmetric | |

**User's choice:** 'Build produced + smoke-tested locally' is enough (Recommended)
**Notes:** Specifically Android. iOS smoke deferred to end-of-milestone release phase.

---

## Claude's Discretion (consolidated)

- Sheet vs full-screen Modal for the "You're leaving the app" interstitial
- `paymentReturnStore` vs extending `authStore` for polling state
- i18n key names within the locked namespaces (`login.*`, `payment.upgrade.*`, etc.)
- AccountScreen "Sign in to sync Pro" card visual treatment
- `useSubscription` hook keep-vs-replace decision post-rewrite
- T-7 mitigation form (`_skipAuthRefresh` flag vs URL-pattern check in interceptor)
- Pre-selected provider routing from AccountScreen → LoginScreen

## Deferred Ideas (consolidated)

- TestFlight + Play Internal upload (end-of-milestone release phase)
- fastlane / CI automation
- iOS smoke-test on physical iPhone
- Universal Links over `vpnapp://` custom scheme
- MMKV migration from AsyncStorage
- Mobile consumption of `plan_id` JWT claim
- ES locale on mobile
- "Merge accounts" UI
- Share-code Pro warning before SSO
- Mobile `cancelSubscription` UI
- Apple external-link entitlement form
- Per-provider error toasts (Claude's Discretion in planning)
- Linked-provider chips on AccountScreen
- Splash on SSO success
