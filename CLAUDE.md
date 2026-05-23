<!-- GSD:project-start source:PROJECT.md -->
## Project

**RiseVPN**

RiseVPN is a consumer VPN service operating on Android (Google Play) and iOS, backed by a single-tenant Go API, Postgres, Redis and a VLESS/REALITY tunnel server. The product is currently free-tier only with anonymous device-based accounts; a Pro tier (paid subscription) is being introduced on the web with lava.top as the sole payment provider, and Apple/Google Sign-In is being added as the primary cross-surface identity so a user pays on the website and gets Pro reflected in the mobile app on next foreground.

**Core Value:** **A user signs in once with Apple or Google, pays on risevpn.com via lava.top, and Pro unlocks on every device immediately.** Everything else (admin tooling, performance work, hardening) serves that path.

### Constraints

- **Tech stack — Backend:** Go 1.25 + Fiber v2 + GORM + Postgres 16 + Redis 7. Locked. No language switch. (Bumped from 1.22 on 2026-05-23: indirect deps require directive >= 1.25 — local `go test` refused to run with 1.22.0 directive.)
- **Tech stack — Mobile:** React Native 0.84, TypeScript, Zustand stores, axios, react-navigation. Locked.
- **Tech stack — Landing:** Next.js 16 App Router + next-intl (EN/RU/ES) + shadcn/ui + Tailwind 4. Locked.
- **Tech stack — Admin web:** Vite + React 19 + TanStack Query + shadcn/ui. Locked.
- **Payment provider:** lava.top exclusively. Single-provider strategy.
- **Identity provider:** Apple + Google SSO (web + mobile). Guest device-based login preserved for "try before sign up".
- **App-store compliance:** No IAP buttons in mobile app. CTA points to risevpn.com.
- **Deployment:** Single VM via Docker Compose for v2.2.0. Horizontal scaling (multi-replica API) is a Tranche 3 goal (`RUN_SCHEDULER` env gate) but not required for launch.
- **Security:** No paying users yet but launching Pro means real money flow — security audit findings classified Critical/High MUST land before any user pays. See `docs/audit/SECURITY-AUDIT.md` "Top 3 must-fix-before-lava-launch".
- **Webhook reliability:** lava.top retries up to 20 times. Webhook handler MUST be idempotent (UNIQUE constraint on event identifier) and MUST return 500 on processing error so retries trigger.
- **lava.top constraints:** 8% commission, minimum $5/€5 per offer, payment URL TTL ~24h, contracts identified by UUID.
<!-- GSD:project-end -->

<!-- GSD:stack-start source:STACK.md -->
## Technology Stack

Technology stack not yet documented. Will populate after codebase mapping or first phase.
<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->
## Conventions

Conventions not yet established. Will populate as patterns emerge during development.
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->
## Architecture

Architecture not yet mapped. Follow existing patterns found in the codebase.
<!-- GSD:architecture-end -->

<!-- GSD:skills-start source:skills/ -->
## Project Skills

No project skills found. Add skills to any of: `.claude/skills/`, `.agents/skills/`, `.cursor/skills/`, or `.github/skills/` with a `SKILL.md` index file.
<!-- GSD:skills-end -->

<!-- GSD:workflow-start source:GSD defaults -->
## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `/gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd-debug` for investigation and bug fixing
- `/gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->



<!-- GSD:profile-start -->
## Developer Profile

> Profile not yet configured. Run `/gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
