---
phase: 3
slug: lava-top-plans-catalog
plan_number: 4
wave: 2
depends_on: [1, 3]
files_modified:
  - server/api/internal/handler/servers.go
  - server/api/internal/handler/connection.go
  - server/api/internal/handler/devices.go
  - server/api/internal/handler/admin.go
  - server/api/internal/handler/health.go
  - server/api/internal/handler/servers_test.go
autonomous: true
requirements_addressed: [PAY-11]
estimated_complexity: high
---

<objective>
Delete every reference to the `model.PlanLimits` map across the handler layer and re-wire to the new repository functions from plan 03-03. Specifically:
- `handler/servers.go::ListServers` — branch on role (admin → ListActiveServers; non-admin → ListServersForPlan).
- `handler/servers.go::GetServerConfig` — add IsServerAllowedForPlan check, return 404 (not 403) on denial per D-22; admins bypass.
- `handler/connection.go::RegisterConnection` — read device limit from `FindPlanByID(planID).MaxDevices` instead of `PlanLimits[tier].MaxDevices`.
- `handler/devices.go::CreateShareCode` + `LinkDevice` device-cap checks — same pattern.
- `handler/admin.go::AdminUpdateUser` — validate `subscription_tier` against `FindPlanByCode` (404 if no such plan), update `users.plan_id` alongside `users.subscription_tier` so the two stay in sync.
- `handler/health.go::GetSubscription` — read system plan via `FindSystemPlanID` + `FindPlanByID` for the no-subscription default; read paid plan limits via `FindPlanByCode(sub.Plan)`.

The `c.Locals("plan_id")` value isn't populated yet (lands in plan 03-07's middleware change); for THIS plan, handlers fall back to `repository.FindUserByID(userID).PlanID` (one DB read per request — acceptable until 03-07's JWT claim lands and the 5-min access TTL bound completes). Document the fallback so plan 03-07 just deletes the fallback when it ships.
</objective>

<context>
@.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md
@.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md
@server/api/internal/handler/servers.go
@server/api/internal/handler/connection.go
@server/api/internal/handler/devices.go
@server/api/internal/handler/admin.go
@server/api/internal/handler/health.go
@server/api/internal/repository/plan_repo.go
</context>

<interfaces>
Functions consumed (all defined in plan 03-03):

```go
func FindUserByID(db *gorm.DB, userID string) (*model.User, error) // existing — returns User with PlanID populated after migration 019
func FindPlanByID(db *gorm.DB, planID string) (*model.Plan, error)
func FindPlanByCode(db *gorm.DB, code string) (*model.Plan, error)
func FindSystemPlanID(db *gorm.DB) (string, error)
func ListServersForPlan(db *gorm.DB, planID string) ([]model.VPNServer, error)
func IsServerAllowedForPlan(db *gorm.DB, planID, serverID string) (bool, error)
func ListActiveServers(db *gorm.DB) ([]model.VPNServer, error) // existing — admin bypass path
```

Helper to add ONCE in this plan (centralised in handler/servers.go for reuse):

```go
// resolveUserPlanID returns the plan_id for the authenticated user. It first
// reads c.Locals("plan_id") (populated by middleware once plan 03-07 lands);
// if empty (Phase 2 JWTs in flight + backward-compat window), falls back to
// repository.FindUserByID(userID).PlanID.
//
// After plan 03-07 ships and the 5-min access-token TTL bound completes, the
// fallback is dead code — but it's cheap (single indexed PK lookup ~0.5ms)
// and structurally safer than failing 500.
func resolveUserPlanID(c *fiber.Ctx, db *gorm.DB) (string, error)
```
</interfaces>

<tasks>

<task type="auto">
  <id>03-04-T01</id>
  <name>Rewire handler/servers.go (ListServers role branch + GetServerConfig 404 on denied)</name>
  <files>server/api/internal/handler/servers.go</files>
  <read_first>
    - server/api/internal/handler/servers.go (CURRENT — line 99-130 ListServers; line 132-262 GetServerConfig)
    - server/api/internal/repository/plan_repo.go (T01 of plan 03-03 — ListServersForPlan + IsServerAllowedForPlan)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-21 (handler-level role branch), D-22 (404 not 403 on denied)
    - docs/ADR-007-lava-sso-rework.md §19.5 (admin bypass pattern: `if role=="admin" { ListActiveServers() } else { ListServersForPlan(planID) }`)
  </read_first>
  <action>
    Three edits to `server/api/internal/handler/servers.go`:

    **(a) ADD the `resolveUserPlanID` helper** at the END of the file (after the existing `protocolPriorityForRegion` function):

```go
// resolveUserPlanID returns the plan_id for the authenticated user.
//
// Until plan 03-07 (JWT plan_id claim) lands, c.Locals("plan_id") is unset —
// fall back to a single DB lookup via FindUserByID. The 5-min access TTL
// bound after 03-07 ships eventually makes the DB fallback dead code, but
// the cost is one indexed PK lookup (~0.5ms) and structurally safer than
// failing 500 on missing claim.
//
// IMPORTANT: after plan 03-07 ships and operators have rolled the JWT shape,
// this helper continues to work — claims.PlanID populates c.Locals via the
// amended middleware. No further code change in this file needed at that point.
func resolveUserPlanID(c *fiber.Ctx, db *gorm.DB) (string, error) {
	if planID, ok := c.Locals("plan_id").(string); ok && planID != "" {
		return planID, nil
	}
	userID, _ := c.Locals("user_id").(string)
	if userID == "" {
		return "", fiber.ErrUnauthorized
	}
	user, err := repository.FindUserByID(db, userID)
	if err != nil {
		return "", err
	}
	return user.PlanID, nil
}
```

    **(b) REPLACE the body of `ListServers`** (currently lines 99-130) — branch on role:

```go
// ListServers handles GET /servers.
// Returns active VPN servers limited by the user's plan.
// Admins bypass the plan filter and see all active servers (PAY-11, D-21).
func ListServers(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals("user_id").(string)
		role, _ := c.Locals("role").(string)

		var servers []model.VPNServer
		var err error
		if role == "admin" {
			// Admin bypass — sees all active servers regardless of plan.
			servers, err = repository.ListActiveServers(db)
		} else {
			planID, perr := resolveUserPlanID(c, db)
			if perr != nil {
				logger.Error("failed to resolve plan_id for ListServers",
					zap.String("user_id", userID), zap.Error(perr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			servers, err = repository.ListServersForPlan(db, planID)
		}
		if err != nil {
			logger.Error("failed to list servers", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		logger.Debug("listing servers",
			zap.String("user_id", userID),
			zap.String("role", role),
			zap.Int("count", len(servers)),
		)
		return c.JSON(fiber.Map{
			"data": servers,
		})
	}
}
```

    **(c) AMEND `GetServerConfig`** — insert the IsServerAllowedForPlan check immediately AFTER `serverID := c.Params("id")` and BEFORE `server, err := repository.FindServerByID(db, serverID)`. Admin bypass via the same role check. On denial: return 404 (D-22 defence-in-depth — don't leak server existence).

```go
		serverID := c.Params("id")
		userID := c.Locals("user_id").(string)
		role, _ := c.Locals("role").(string)

		// PAY-11 / D-22: enforce server-access at the handler layer before
		// fetching the server. Admins bypass. Returns 404 (NOT 403) on denial
		// so a lower-tier user can't enumerate paid-tier server UUIDs.
		if role != "admin" {
			planID, perr := resolveUserPlanID(c, db)
			if perr != nil {
				logger.Error("failed to resolve plan_id for GetServerConfig",
					zap.String("user_id", userID), zap.Error(perr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			allowed, aerr := repository.IsServerAllowedForPlan(db, planID, serverID)
			if aerr != nil {
				logger.Error("failed to check plan-server pairing",
					zap.String("plan_id", planID), zap.String("server_id", serverID), zap.Error(aerr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			if !allowed {
				logger.Info("server not allowed for plan — returning 404 (defence in depth)",
					zap.String("user_id", userID), zap.String("plan_id", planID), zap.String("server_id", serverID))
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "server not found",
				})
			}
		}

		server, err := repository.FindServerByID(db, serverID)
```

    The existing post-`server` code (server.IsActive check, config building, REALITY/WS/AWG branches) remains unchanged.

    Delete the `"vpnapp/server/api/internal/model"` import if it's no longer used by this file (the `model.PlanLimits` reference was the only use); GORM/zap/fiber imports stay. Run `cd server/api && go build ./internal/handler/...` and `goimports -w server/api/internal/handler/servers.go` (or remove the import manually).
  </action>
  <acceptance_criteria>
    - `grep -c "model.PlanLimits" server/api/internal/handler/servers.go` returns 0
    - `grep "ListServersForPlan" server/api/internal/handler/servers.go` finds at least one match
    - `grep "IsServerAllowedForPlan" server/api/internal/handler/servers.go` finds at least one match
    - `grep "resolveUserPlanID" server/api/internal/handler/servers.go` finds at least 3 matches (declaration + 2 callers: ListServers, GetServerConfig)
    - `grep -E 'StatusNotFound.*server not found' server/api/internal/handler/servers.go` finds at least one match in the denied branch (D-22)
    - `grep "role == \"admin\"" server/api/internal/handler/servers.go` finds at least 2 matches (ListServers + GetServerConfig admin bypass)
    - `cd server/api && go build ./internal/handler/...` exits 0
    - `cd server/api && go vet ./internal/handler/...` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./internal/handler/... && go vet ./internal/handler/...</automated>
  <done>servers.go has no PlanLimits references; ListServers branches on role; GetServerConfig returns 404 on plan-denied (D-22).</done>
</task>

<task type="auto">
  <id>03-04-T02</id>
  <name>Rewire handler/connection.go and handler/devices.go (device-limit reads via FindPlanByID)</name>
  <files>
    server/api/internal/handler/connection.go,
    server/api/internal/handler/devices.go
  </files>
  <read_first>
    - server/api/internal/handler/connection.go (CURRENT — lines 80-150, particularly lines 97-100 with PlanLimits[tier] device-limit read)
    - server/api/internal/handler/devices.go (CURRENT — lines 100-115 CreateShareCode cap check + lines 250-270 LinkDevice cap check)
    - server/api/internal/handler/servers.go (just amended in T01 — `resolveUserPlanID` lives here for reuse)
    - server/api/internal/repository/plan_repo.go (T01 of plan 03-03 — FindPlanByID + FindSystemPlanID)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-24 (connection.go reads device limits from FindPlanByID(planID); ADR-style fallback)
  </read_first>
  <action>
    Two file edits.

    **(a) `server/api/internal/handler/connection.go`** — replace the `model.PlanLimits[tier]` block (currently lines 94-101) with a plan-based lookup. The block currently reads:

    ```go
    limits, ok := model.PlanLimits[tier]
    if !ok {
        limits = model.PlanLimits["free"]
    }
    ```

    Replace with:

    ```go
    // Read device limit from the plan row via plan_id (PAY-11 / D-24).
    // resolveUserPlanID handles the JWT-claim-vs-DB-fallback during the
    // plan 03-07 backward-compat window.
    planID, perr := resolveUserPlanID(c, db)
    if perr != nil {
        logger.Error("failed to resolve plan_id for RegisterConnection",
            zap.String("user_id", userID), zap.Error(perr))
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "error": "internal server error",
        })
    }
    plan, perr := repository.FindPlanByID(db, planID)
    if perr != nil {
        // Fallback to system plan limits on a stale-plan_id row — fail safe.
        logger.Warn("FindPlanByID failed; falling back to system plan",
            zap.String("plan_id", planID), zap.Error(perr))
        systemPlanID, sperr := repository.FindSystemPlanID(db)
        if sperr != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
                "error": "internal server error",
            })
        }
        plan, sperr = repository.FindPlanByID(db, systemPlanID)
        if sperr != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
                "error": "internal server error",
            })
        }
    }
    // Compose a "limits" struct that the existing code below can keep using.
    limits := struct {
        MaxDevices int
        MaxServers int
    }{MaxDevices: plan.MaxDevices, MaxServers: plan.MaxServers}
    _ = tier // keep the variable around if it's still read below for logs
    ```

    The original `tier` variable comes from `c.Locals("tier").(string)` near line 60; leave that line in place so subsequent log statements still have `tier` for context. The downstream `limits.MaxDevices` reads / `if limits.MaxDevices == model.UnlimitedDevices` checks continue to work as-is because the struct shape matches.

    **(b) `server/api/internal/handler/devices.go`** — two call sites need rewriting. Both read `model.PlanLimits[tier]` to enforce the device-count cap.

    **Call site 1: `CreateShareCode` (around line 100-110):**
    Replace:
    ```go
    tier := user.SubscriptionTier
    if tier == "" {
        tier = "free"
    }
    limits, ok := model.PlanLimits[tier]
    if !ok {
        limits = model.PlanLimits["free"]
    }
    ```
    with:
    ```go
    // PAY-11 / D-24: read device cap from the plan row via the user's plan_id.
    plan, perr := repository.FindPlanByID(db, user.PlanID)
    if perr != nil {
        logger.Warn("CreateShareCode: FindPlanByID failed; falling back to system plan",
            zap.String("plan_id", user.PlanID), zap.Error(perr))
        systemPlanID, sperr := repository.FindSystemPlanID(db)
        if sperr != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
                "error": "internal server error",
            })
        }
        plan, sperr = repository.FindPlanByID(db, systemPlanID)
        if sperr != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
                "error": "internal server error",
            })
        }
    }
    limits := struct {
        MaxDevices int
        MaxServers int
    }{MaxDevices: plan.MaxDevices, MaxServers: plan.MaxServers}
    ```

    **Call site 2: `LinkDevice` inside the existing transaction (around line 253-260):**
    Same replacement, but the transaction uses `tx` not `db`. Inside the `db.Transaction(func(tx *gorm.DB) error { ... })` block:
    Replace:
    ```go
    tier := owner.SubscriptionTier
    if tier == "" {
        tier = "free"
    }
    limits, ok := model.PlanLimits[tier]
    if !ok {
        limits = model.PlanLimits["free"]
    }
    ```
    with:
    ```go
    // PAY-11 / D-24: read device cap from the owner's plan row via the tx.
    plan, perr := repository.FindPlanByID(tx, owner.PlanID)
    if perr != nil {
        logger.Warn("LinkDevice: FindPlanByID failed; falling back to system plan",
            zap.String("plan_id", owner.PlanID), zap.Error(perr))
        systemPlanID, sperr := repository.FindSystemPlanID(tx)
        if sperr != nil {
            return fmt.Errorf("link: find system plan: %w", sperr)
        }
        plan, sperr = repository.FindPlanByID(tx, systemPlanID)
        if sperr != nil {
            return fmt.Errorf("link: find system plan row: %w", sperr)
        }
    }
    limits := struct {
        MaxDevices int
        MaxServers int
    }{MaxDevices: plan.MaxDevices, MaxServers: plan.MaxServers}
    ```

    After both edits run `cd server/api && go build ./internal/handler/...`. If `tier` is no longer referenced in connection.go after the replacement, remove its declaration AND the `_ = tier` filler line. Delete the `"vpnapp/server/api/internal/model"` import if it's now unused (it likely still IS used in devices.go for `model.LinkCode`, `model.Device`, `model.UnlimitedDevices` — keep the import in that case).
  </action>
  <acceptance_criteria>
    - `grep -c "model.PlanLimits" server/api/internal/handler/connection.go` returns 0
    - `grep -c "model.PlanLimits" server/api/internal/handler/devices.go` returns 0
    - `grep "FindPlanByID" server/api/internal/handler/connection.go` finds at least one match
    - `grep "FindPlanByID" server/api/internal/handler/devices.go` finds at least 2 matches (CreateShareCode + LinkDevice)
    - `grep "FindSystemPlanID" server/api/internal/handler/connection.go server/api/internal/handler/devices.go` finds at least 3 matches (fallback in 3 sites)
    - `grep "model.UnlimitedDevices" server/api/internal/handler/connection.go` finds matches (the sentinel constant is still in use AFTER the rewrite — we just changed where MaxDevices comes from)
    - `cd server/api && go build ./internal/handler/...` exits 0
    - `cd server/api && go test ./internal/handler/ -run "TestRegisterConnection|TestCreateShareCode|TestLinkDevice" -count=1 -timeout=60s` exits 0 (existing tests still pass against the new plan_id-driven path; some tests may need test fixtures updated — see T05)
  </acceptance_criteria>
  <automated>cd server/api && go build ./internal/handler/...</automated>
  <done>Neither connection.go nor devices.go references PlanLimits; both fall through to FindSystemPlanID on plan_id lookup failure.</done>
</task>

<task type="auto">
  <id>03-04-T03</id>
  <name>Rewire handler/admin.go (AdminUpdateUser validates against plans table + writes users.plan_id)</name>
  <files>server/api/internal/handler/admin.go</files>
  <read_first>
    - server/api/internal/handler/admin.go (CURRENT — lines 136-143 PlanLimits validation; line 142 updates["subscription_tier"] write)
    - server/api/internal/repository/plan_repo.go (T01 of plan 03-03 — FindPlanByCode)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-24 (admin.go validates against FindPlanByCode — returns 404 if no such plan)
  </read_first>
  <action>
    Edit `server/api/internal/handler/admin.go`. The `AdminUpdateUser` handler's subscription_tier validation currently reads (~line 136-143):

    ```go
    if req.SubscriptionTier != "" {
        if _, ok := model.PlanLimits[req.SubscriptionTier]; !ok {
            return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
                "error": "subscription_tier must be one of: free, premium, ultimate",
            })
        }
        updates["subscription_tier"] = req.SubscriptionTier
    }
    ```

    Replace with:

    ```go
    if req.SubscriptionTier != "" {
        // PAY-11 / D-24: validate tier against the plans table (no hardcoded enum).
        // Lookup BY CODE — both active and inactive plans resolve so admins can
        // re-attach a user to a soft-deleted plan if needed (grandfathering use case).
        plan, perr := repository.FindPlanByCode(db, req.SubscriptionTier)
        if perr != nil {
            if errors.Is(perr, repository.ErrNotFound) {
                return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
                    "error": "subscription_tier must match an existing plan code",
                })
            }
            logger.Error("AdminUpdateUser: FindPlanByCode failed",
                zap.String("code", req.SubscriptionTier), zap.Error(perr))
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
                "error": "internal server error",
            })
        }
        updates["subscription_tier"] = req.SubscriptionTier
        // Keep users.plan_id in sync with the denormalised tier string.
        updates["plan_id"] = plan.ID
    }
    ```

    Confirm the `errors` import is in the file's import block (it almost certainly already is — admin.go uses `errors.Is(err, repository.ErrNotFound)` elsewhere). If not, add `"errors"` to the imports.

    Remove the `"vpnapp/server/api/internal/model"` import if it becomes unused; otherwise keep it (admin.go may reference `model.User`, etc. elsewhere). Run `cd server/api && go build ./internal/handler/...` to confirm.
  </action>
  <acceptance_criteria>
    - `grep -c "model.PlanLimits" server/api/internal/handler/admin.go` returns 0
    - `grep "FindPlanByCode" server/api/internal/handler/admin.go` finds one match
    - `grep "updates\\[\"plan_id\"\\]" server/api/internal/handler/admin.go` finds one match (admin writes plan_id alongside tier)
    - `grep 'errors.Is(perr, repository.ErrNotFound)' server/api/internal/handler/admin.go` finds matches (existing pattern preserved)
    - `cd server/api && go build ./internal/handler/...` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./internal/handler/...</automated>
  <done>admin.go validates subscription_tier against plans table; admin updates write both subscription_tier AND plan_id atomically.</done>
</task>

<task type="auto">
  <id>03-04-T04</id>
  <name>Rewire handler/health.go::GetSubscription (system plan default + paid plan via FindPlanByCode)</name>
  <files>server/api/internal/handler/health.go</files>
  <read_first>
    - server/api/internal/handler/health.go (CURRENT — lines 33-67 GetSubscription with PlanLimits["free"] default + PlanLimits[sub.Plan] paid path)
    - server/api/internal/repository/plan_repo.go (T01 of plan 03-03 — FindSystemPlanID + FindPlanByID + FindPlanByCode)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-24 (health.go reads defaults from system plan)
  </read_first>
  <action>
    Edit `server/api/internal/handler/health.go`. The current `GetSubscription` (lines 33-68) has two `PlanLimits` reads. Replace both.

    Find and replace the body of `GetSubscription`:

```go
// GetSubscription handles GET /subscription.
// Returns the user's active subscription from the database.
//
// PAY-11 / D-24: defaults come from the system plan (via FindSystemPlanID); paid-plan
// limits come from FindPlanByCode(sub.Plan). No more hardcoded PlanLimits map.
func GetSubscription(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		sub, err := repository.FindSubscriptionByUserID(db, userID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				// No subscription — return system-plan defaults.
				systemPlanID, sperr := repository.FindSystemPlanID(db)
				if sperr != nil {
					logger.Error("GetSubscription: FindSystemPlanID failed", zap.Error(sperr))
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"error": "internal server error",
					})
				}
				systemPlan, sperr := repository.FindPlanByID(db, systemPlanID)
				if sperr != nil {
					logger.Error("GetSubscription: FindPlanByID(system) failed", zap.Error(sperr))
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"error": "internal server error",
					})
				}
				return c.JSON(fiber.Map{
					"data": fiber.Map{
						"plan":        systemPlan.Code,
						"is_active":   true,
						"max_devices": systemPlan.MaxDevices,
					},
				})
			}
			logger.Error("failed to get subscription", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		// Active subscription — read limits from the matching plan row.
		plan, perr := repository.FindPlanByCode(db, sub.Plan)
		if perr != nil {
			// Plan was soft-deleted — fall back to system plan for the limits
			// (the user keeps Pro until expiry per ADR §19.10; display the
			// numeric limits from the system plan to avoid 500).
			logger.Warn("GetSubscription: FindPlanByCode failed; falling back to system plan",
				zap.String("plan", sub.Plan), zap.Error(perr))
			systemPlanID, sperr := repository.FindSystemPlanID(db)
			if sperr != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			plan, sperr = repository.FindPlanByID(db, systemPlanID)
			if sperr != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
		}

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"id":          sub.ID,
				"plan":        sub.Plan,
				"is_active":   sub.IsActive,
				"started_at":  sub.StartedAt,
				"expires_at":  sub.ExpiresAt,
				"max_devices": plan.MaxDevices,
			},
		})
	}
}
```

    Delete the `"vpnapp/server/api/internal/model"` import if it's no longer referenced (the file has `model.PlanLimits` removed; the only other model use was `model.PlanLimits` — verify before removing the import).
  </action>
  <acceptance_criteria>
    - `grep -c "model.PlanLimits" server/api/internal/handler/health.go` returns 0
    - `grep "FindSystemPlanID" server/api/internal/handler/health.go` finds at least 2 matches (default branch + fallback branch)
    - `grep "FindPlanByCode" server/api/internal/handler/health.go` finds one match (paid-plan path)
    - `cd server/api && go build ./internal/handler/...` exits 0
    - `cd server/api && go test ./internal/handler/ -run "TestGetSubscription" -count=1 -timeout=30s` exits 0 (existing tests may need test fixtures — see T05)
  </acceptance_criteria>
  <automated>cd server/api && go build ./internal/handler/...</automated>
  <done>health.go GetSubscription reads everything from the plans table; no PlanLimits references.</done>
</task>

<task type="auto">
  <id>03-04-T05</id>
  <name>Update servers_test.go and any other handler tests broken by the PlanLimits removal (sqlite test schemas need plans + plan_servers + users.plan_id)</name>
  <files>
    server/api/internal/handler/servers_test.go
  </files>
  <read_first>
    - server/api/internal/handler/servers_test.go (CURRENT — read the test-local CREATE TABLE statements + the test helpers that seed users + servers)
    - server/api/internal/handler/connection_test.go (sibling — may also need plans seeded; verify by running tests)
    - server/api/internal/handler/devices_test.go (sibling)
    - server/api/internal/handler/admin_test.go (sibling)
    - server/api/internal/handler/auth_test.go (sibling)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §9.1 (test schemas need plans + plan_servers + plan_id columns; SQLite can't gen_random_uuid — pass explicit UUIDs)
  </read_first>
  <action>
    The PlanLimits removal in T01-T04 changes the handler test surface — handlers now query `plans`, `plan_servers`, and `users.plan_id` instead of the in-memory map. Test-local sqlite schemas must be updated to include these tables/columns, and seed helpers must populate them.

    **Step 1: Run all handler tests to identify failures:**
    ```
    cd server/api && go test ./internal/handler/ -count=1 -timeout=120s 2>&1 | tee /tmp/handler-test-output.log
    ```
    Expect failures in `servers_test.go`, `connection_test.go`, `devices_test.go`, `admin_test.go`, and possibly `auth_test.go` / `health` tests (if any). Common failure modes:
    - "no such table: plans" — schema doesn't have the new tables.
    - "no such column: plan_id" — schema doesn't have users.plan_id.
    - "ErrNotFound" from FindPlanByID — seed didn't insert the user's plan_id row.

    **Step 2: For `servers_test.go` specifically (the file modified in T01), update the schema + seed.** Read the file's `setupDB` / `setupServerTestDB` helper. Add to the `CREATE TABLE users` DDL the column:
    ```sql
    plan_id TEXT NOT NULL DEFAULT '',
    ```
    Append a CREATE TABLE block for plans + plan_servers (use the same schema as `setupPlanRepoDB` in plan 03-03 / T02 — copy verbatim).

    For each test helper that creates a user, after the INSERT add a `_, _ = repository.SetUserPlan(db, userID, planID, nil, nil)` — but BEFORE that the test setup must `db.Create(&model.Plan{ID: planID, Code: "free", IsSystem: true, IsActive: true, MaxDevices: 1, MaxServers: 3})` to populate the plans table.

    Recommended pattern: add a `seedPlansAndAssignFree(t, db, userIDs ...string)` helper at the top of the test file that:
    1. Creates `free` (is_system=true) and `pro` plans.
    2. Calls `SetUserPlan(db, userID, freePlanID, nil, nil)` for each user passed in.
    3. Returns the two plan UUIDs so test bodies can flip a user to pro via SetUserPlan when needed.

    Then `TestListServers_*` (every variant) calls `seedPlansAndAssignFree(t, db, user.ID)` after seeding the user. For `TestListServers_AdminBypass` (or rename — see acceptance criteria), set `Role: "admin"` on the user and seed servers WITHOUT plan_servers pairings — confirm the admin still sees all servers.

    Add or rename a test to match the 03-VALIDATION.md PAY-11 named test: `TestListServers_AdminBypass` (admin role with a tight plan still sees all active servers).

    **Step 3: For `connection_test.go`, `devices_test.go`, `admin_test.go`, `auth_test.go`** — apply the same schema augmentation pattern. The test files own their schemas; THIS plan ONLY adds the plans + plan_servers tables and the users.plan_id column, with a free-plan seed. Existing assertions about device limits / share-code caps continue to assert against `max_devices=1` (free plan default — matches what the old `PlanLimits["free"].MaxDevices` was).

    **Step 4: Run `cd server/api && go test ./internal/handler/ -count=1 -timeout=120s` and confirm GREEN.** Iterate on individual failures.

    **Step 5: Confirm the planner-required PAY-11 named test exists by name:** the test that validates "admin sees all servers regardless of plan" must be named `TestListServers_AdminBypass` (per 03-VALIDATION.md PAY-11 row). If the existing file has a different name, rename it.

    This task is LARGER than the surface area indicates because it touches 4-5 test files. The mechanical action is: open each, add plans + plan_servers + users.plan_id schema augmentation, add a `seedPlansAndAssignFree` helper call before any handler invocation that needs the new path, run tests until green.

    **Recovery hint:** if a test asserts a specific HTTP body shape (e.g. `expected 2 servers, got 3`), it's checking the old `PlanLimits["free"].MaxServers=3` slicing. The new path is plan-driven — ListServersForPlan returns whatever plan_servers attaches. Update the test to seed plan_servers explicitly: for a free-tier user expecting 3 servers visible, the test must insert 3 (server, free_plan) rows into plan_servers.
  </action>
  <acceptance_criteria>
    - `grep "plan_id TEXT" server/api/internal/handler/servers_test.go` finds at least one match (schema augmentation)
    - `grep "CREATE TABLE plans" server/api/internal/handler/servers_test.go` finds at least one match
    - `grep "TestListServers_AdminBypass" server/api/internal/handler/servers_test.go` finds one match (PAY-11 named test from 03-VALIDATION.md)
    - `cd server/api && go test ./internal/handler/ -count=1 -timeout=180s` exits 0 (all handler tests pass — connection, devices, admin, auth, health, servers)
    - `cd server/api && go vet ./...` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/handler/ -count=1 -timeout=180s</automated>
  <done>All handler tests pass after PlanLimits removal; servers_test.go has TestListServers_AdminBypass; sqlite test schemas augmented with plans + plan_servers tables and users.plan_id column.</done>
</task>

</tasks>

<verification>
- `cd server/api && go build ./...` exits 0
- `cd server/api && go vet ./...` exits 0
- `cd server/api && go test ./internal/handler/ -count=1 -timeout=180s` exits 0
- `grep -rn "model.PlanLimits" server/api/internal/handler/` returns 0 hits (entire handler package free of the dead map)
- `grep -rn "model.PlanLimits" server/api/` returns 0 hits OUTSIDE the migration test (which intentionally does NOT reference it either — verify)
- `grep "TestListServers_AdminBypass" server/api/internal/handler/servers_test.go` finds one match (PAY-11 evidence)
</verification>

<must_haves>
truths:
  - "handler/servers.go ListServers branches on role — admin → ListActiveServers; non-admin → ListServersForPlan(planID)."
  - "handler/servers.go GetServerConfig returns 404 (not 403) when IsServerAllowedForPlan returns false (D-22 defence-in-depth)."
  - "handler/connection.go RegisterConnection reads MaxDevices from FindPlanByID(planID), falls back to system plan if missing."
  - "handler/devices.go CreateShareCode + LinkDevice cap checks use FindPlanByID via user.PlanID (LinkDevice does it inside the existing transaction)."
  - "handler/admin.go AdminUpdateUser validates subscription_tier via FindPlanByCode and writes BOTH subscription_tier AND plan_id in the updates map."
  - "handler/health.go GetSubscription reads defaults from FindSystemPlanID + FindPlanByID(system); paid-plan limits from FindPlanByCode(sub.Plan)."
  - "resolveUserPlanID helper centralised in handler/servers.go reads c.Locals('plan_id') first (post-03-07), falls back to FindUserByID DB lookup."
  - "All sqlite-backed handler tests pass with new plans + plan_servers + users.plan_id schemas; PAY-11 evidence test named TestListServers_AdminBypass."
artifacts:
  - path: "server/api/internal/handler/servers.go"
    provides: "Role-branching ListServers + plan-checked GetServerConfig + resolveUserPlanID helper"
    contains: "ListServersForPlan"
  - path: "server/api/internal/handler/connection.go"
    provides: "Plan-driven device limit enforcement"
    contains: "FindPlanByID"
  - path: "server/api/internal/handler/health.go"
    provides: "System-plan-default GetSubscription"
    contains: "FindSystemPlanID"
key_links:
  - from: "server/api/internal/handler/servers.go::GetServerConfig"
    to: "server/api/internal/repository/plan_repo.go::IsServerAllowedForPlan"
    via: "Pre-check before FindServerByID; 404 on false"
    pattern: "IsServerAllowedForPlan\\(db, planID, serverID\\)"
  - from: "server/api/internal/handler/admin.go::AdminUpdateUser"
    to: "server/api/internal/repository/plan_repo.go::FindPlanByCode"
    via: "Validates subscription_tier input + writes plan_id"
    pattern: "FindPlanByCode\\(db, req.SubscriptionTier\\)"
</must_haves>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| Client → /servers, /servers/:id/config | Client-provided server UUID can be anything; the IsServerAllowedForPlan check enforces plan boundary at the handler. |
| Admin → /admin/users/:id | Admin-supplied subscription_tier is validated against the plans table (no client-controlled tier elevation). |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-23 | Information disclosure | GetServerConfig leaks server existence to lower tiers | mitigate | 404 (not 403) on IsServerAllowedForPlan=false (D-22). A free-tier user probing for paid-only server UUIDs gets the same response as for a non-existent server. |
| T-03-24 | Elevation of Privilege | Free user enumerates paid plan's servers via /servers | mitigate | ListServersForPlan filters server-side via JOIN on plan_servers; non-admin clients never see servers not in their plan. Admin bypass is explicit via `role == "admin"` check on c.Locals; AdminRequired middleware enforces role earlier. |
| T-03-25 | Elevation of Privilege | Admin sets subscription_tier="root" via /admin/users | mitigate | FindPlanByCode is the only path that can set updates["subscription_tier"]; unknown codes return 400. plan_id is co-updated atomically with the tier string. |
| T-03-26 | Tampering | Stale users.plan_id row references a soft-deleted plan | mitigate | FindPlanByID falls back to system plan on miss in all 5 handler call sites (connection, devices×2, health, ...). GetSubscription also falls back to system plan for limits if the plan is soft-deleted. Fail-safe default — user never sees a 500 from a deleted-plan reference. |
| T-03-27 | Information disclosure | resolveUserPlanID DB read on every protected request | accept | One indexed PK lookup per request during the 5-min backward-compat window after plan 03-07 ships. RESEARCH §7.5 confirms middleware already does a FindUserByID for HOTFIX-02; we could optimise but the cost is bounded and the optimisation is plan 03-07's middleware change. |
| T-03-28 | Elevation of Privilege | Admin bypass via c.Locals("role") manipulation | accept | role is set by AuthRequired middleware from the validated JWT — not client-controllable. AdminRequired middleware (HOTFIX-02) re-reads role from DB on every admin request. The handler's `role == "admin"` check is read-only; a non-admin can never set this local. |

ASVS L2 scoping per D-31: this plan touches /servers (L1) + /admin/users (L2). All L2 controls applied: V4 access control (plan-based ACL), V5 input validation (FindPlanByCode rejects invalid tier names), V13 API contract (404 on denied per D-22).
</threat_model>

<success_criteria>
1. `cd server/api && go build ./...` exits 0.
2. `cd server/api && go vet ./...` exits 0.
3. `cd server/api && go test ./internal/handler/ -count=1 -timeout=180s` exits 0.
4. `grep -rn 'model.PlanLimits' server/api/internal/handler/` returns 0 hits.
5. `grep "TestListServers_AdminBypass" server/api/internal/handler/servers_test.go` finds one match.
6. PAY-11 verified — server-access filter at repository layer is enforced at the handler layer; admin bypass works.
</success_criteria>

<output>
T01..T05 land as 5 atomic commits (`feat(03-04): ...`); planner commits this plan file once with `docs(03): plan server-access-enforcement`.
</output>
