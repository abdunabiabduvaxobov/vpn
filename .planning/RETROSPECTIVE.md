# RiseVPN — Living Retrospective

## Milestone: v2.2.0 — Lava + SSO

**Shipped:** 2026-06-03
**Phases:** 8 | **Plans:** 71 | **Tasks:** 153 | **Timeline:** 12 days (2026-05-22 → 06-03)

### What Was Built
Apple/Google SSO backend with stable cross-surface identity; lava.top payment integration (dynamic plans catalog + idempotent webhook + Pro-grant under advisory lock); landing money-flow surfaces (login/pricing/pay-success with HttpOnly-cookie proxy); mobile SSO + no-IAP Pro CTA + Keychain token storage; per-user VLESS UUIDs with real tunnel-side wire enforcement; opaque device-bound refresh tokens; full Stripe removal; performance caching layer; admin panel overhaul (KPIs, controls, webhook replay); and 8 critical pre-launch hotfixes.

### What Worked
- **Wave-based parallel execution** (Phase 8: 3 waves, up to 5 agents in isolated worktrees) compressed wall-clock substantially. The executor branch-check (`git reset --hard <base>`) was load-bearing — worktrees kept being created from a divergent phase-01 base, and the check prevented merging ~92k lines of unrelated drift.
- **RED-first Nyquist test infra** (a dedicated Wave-0 plan per phase) gave every HARD/PERF/ADMIN requirement an automated assertion, so verification was evidence-based rather than vibes.
- **Per-user advisory lock (`WithUserLock`) as a shared seam** across webhook + admin paths prevented hybrid subscription state, proven by a real-Postgres race test.
- **The milestone audit earned its keep** — it caught a critical mobile-SSO contract bug (camelCase backend vs snake_case app) that broke the entire core value and that no single-phase verification could surface (Phase 2 tested backend-only, Phase 5 mocked axios).

### What Was Inefficient
- **Cross-worktree duplicate-test conflicts**: parallel agents each re-authored the same Wave-0 test files (absent in their isolated worktrees), producing add/add merge conflicts that needed manual resolution + a flawed-harness fix. Future: seed Wave-0 tests to main before fanning out implementation waves.
- **Sandbox can't run node/jest/playwright or link the tunnel test binary**, so phases 04/05 validation and the tunnel-side HARD-02 test are artifact-based, not live re-runs.
- **Sub-agent reliability**: the `gsd-security-auditor` and `gsd-nyquist-auditor` emitted tool calls as plain text (hallucinated findings, e.g. "all source files are 0-byte"), and `gsd-integration-checker` wasn't installed. Every agent finding had to be independently verified against real code — which is what caught a false-positive (recurring-renewal UUID) and confirmed the real ones.

### Patterns Established
- Snake_case JSON request bodies across the whole API (the SSO structs were the lone camelCase deviation — now fixed).
- Plan changes rotate/revoke the per-user VLESS UUID inside the same lock (webhook payment.success, admin tier-change, admin cancel); pure renewals deliberately don't (D-07, avoid churning live connections).
- Fail-closed on security-critical limiters (link attempts → 503 on Redis outage), fail-open on cost limiters (/debug/error).

### Key Lessons
- **Cross-phase contracts need an integration/E2E check** — unit-testing each side in isolation passes while the wire between them is broken.
- **Verify agent output against source** — don't trust a sub-agent's findings (or its "evidence") without a grep; treat structured-output failures as hallucination risk.
- **`human_needed` ≠ done** — automated coverage can be complete while live-credential/on-device UAT remains the real-world confidence gate before paying users.

### Cost Observations
- Model mix: orchestrator + executors on opus; verifier/auditor agents on sonnet.
- Notable: the most expensive surprises were operational (worktree base divergence, agent tool-call malfunctions), not the implementation itself.

---

## Cross-Milestone Trends

*(First milestone — trends accrue from v2.3.0 onward.)*

| Milestone | Phases | Plans | Days | Critical audit gaps caught |
|-----------|--------|-------|------|----------------------------|
| v2.2.0 Lava + SSO | 8 | 71 | 12 | 1 (mobile SSO contract) |
