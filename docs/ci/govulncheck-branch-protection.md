# govulncheck Branch Protection Runbook (HARD-09 / SC#3)

**Status:** ⬜ MANUAL STEP PENDING — operator must perform the GitHub repo-settings
toggle and the deliberate-vuln proof below, then record the outcome.

## Why this doc exists

`.github/workflows/ci.yml` runs `govulncheck` on every PR that touches
`server/**` and the job **fails (exits non-zero)** when a vulnerable, reachable
dependency is present. That makes the check turn **red**, but a red check does
**not block merge by itself**. Making it merge-blocking requires marking the
check as a **required status check** in GitHub branch protection — a repo-settings
toggle the codebase cannot perform. Without it, SC#3 ("a vuln-introducing PR is
unmergeable") silently ships advisory-only. This runbook closes that gap.

Threat coverage:
- **T-08-09** (Tampering/Elevation): a PR introducing a vulnerable dependency.
- **T-08-09b** (Repudiation): the check being non-blocking/advisory-only.

## Part 1 — Enable the required status check (one-time, GitHub UI)

1. Go to the repository on GitHub → **Settings** → **Branches**.
2. Under **Branch protection rules**, add a rule for `main` (or edit the
   existing one).
3. Enable **"Require status checks to pass before merging"**.
4. In the status-checks search box, select **both** checks as required:
   - `govulncheck-api`
   - `govulncheck-tunnel`

   > Note: a status check only appears in this list **after it has run at least
   > once** on a PR. If the names are not yet selectable, open any small PR that
   > touches `server/**` (or the deliberate-vuln PR in Part 2) so the checks
   > register, then return here to mark them required.
5. (Recommended) Also enable **"Require branches to be up to date before merging"**
   so the gate runs against the merge result.
6. Save the rule.

## Part 2 — Prove it blocks (deliberate-vuln PR)

1. Create a branch off `main`.
2. In `server/api`, introduce a dependency with a **known GO advisory** so the
   Go vuln DB flags it. Pick a version explicitly listed in
   <https://pkg.go.dev/vuln/> (for example, an old `golang.org/x/...` release
   such as a pre-fix `golang.org/x/net` or `golang.org/x/crypto` version, or
   another module/version that `govulncheck` reports as vulnerable). Ensure it
   is **imported and reachable** from `server/api` code (govulncheck only flags
   advisories with a reachable call path), then run `go mod tidy` so
   `go.mod`/`go.sum` reference it.
3. Push the branch and open a PR into `main`.
4. Confirm:
   - The **`govulncheck-api`** check turns **red** (job fails on the finding).
   - With the required-check rule from Part 1 enabled, the **merge button is
     blocked** ("Required statuses must pass before merging").
5. **Revert / close** the deliberate-vuln PR (do not merge it). Delete the
   branch.

## Part 3 — Record the proof

Fill this in after running Part 2. This is the evidence that SC#3 is enforced,
not advisory-only.

| Field | Value |
|-------|-------|
| Branch protection on `main` enabled | ⬜ (yes / no) |
| `govulncheck-api` marked required | ⬜ (yes / no) |
| `govulncheck-tunnel` marked required | ⬜ (yes / no) |
| Deliberate-vuln PR URL | ⬜ (paste PR link) |
| `govulncheck-api` check turned red | ⬜ (yes / no) |
| Merge button blocked | ⬜ (yes / no) |
| Screenshot / evidence path | ⬜ (optional) |
| Deliberate-vuln PR closed/reverted | ⬜ (yes / no) |
| Verified by / date | ⬜ |

## Suppression policy (reminder)

`golang/govulncheck-action` has **no built-in silencing flag**. To clear a
finding, **fix the dependency** (upgrade or replace). Only as an absolute last
resort for a genuinely unfixable advisory, swap that single job to a direct
`govulncheck` invocation with an explicit, commented allowlist — never
blanket-disable the gate. See the header comment in
`.github/workflows/ci.yml`.
