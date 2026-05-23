---
phase: 3
slug: lava-top-plans-catalog
plan_number: 1
wave: 1
depends_on: []
files_modified:
  - server/api/migrations/019_plans_catalog.sql
  - server/api/migrations/020_lava_payments.sql
  - server/api/migrations/migrations_test.go
  - server/api/internal/model/plan.go
  - server/api/internal/model/subscription.go
  - server/api/internal/model/user.go
  - server/api/internal/model/invoice.go
  - server/api/internal/model/lava_contract.go
  - server/api/internal/model/lava_webhook_event.go
  - server/api/internal/repository/subscription_repo.go
  - server/api/internal/repository/subscription_repo_test.go
  - server/api/internal/handler/payment_test.go
  - server/api/go.mod
  - server/api/go.sum
autonomous: true
requirements_addressed: [PAY-01]
estimated_complexity: high
---

<objective>
Land the dynamic plans catalog schema (migration 019), the lava payments schema (migration 020) and matching GORM models so every downstream task in this phase has a stable DB + model surface. Resolve the D-01 ↔ D-03 hidden conflict (RESEARCH §14): removing `model.Subscription.StripeID` breaks `payment_test.go:112` and `subscription_repo.go`, so this plan ALSO rewrites `subscription_repo.go` (drops `FindSubscriptionByStripeID`, rewrites `CreateOrUpdateSubscription` to use `lava_contract_id`), updates `subscription_repo_test.go` to assert against `LavaContractID`, and deletes the orphan Stripe assertions in `payment_test.go` so the build stays green after migration 020 drops `subscriptions.stripe_id`.
</objective>

<context>
@.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md
@.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md
@docs/ADR-007-lava-sso-rework.md
@server/api/internal/model/subscription.go
@server/api/internal/model/user.go
@server/api/internal/repository/subscription_repo.go
</context>

<interfaces>
Existing types the executor MUST preserve (do not change column shape):

```go
// From internal/model/subscription.go (current — REWRITE per this plan)
type Subscription struct {
    ID        string     `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    UserID    string     `json:"user_id" gorm:"not null;index"`
    Plan      string     `json:"plan" gorm:"not null;default:free"`
    StripeID  string     `json:"-" gorm:"type:varchar(255)"`  // REMOVE THIS FIELD
    IsActive  bool       `json:"is_active" gorm:"default:true"`
    StartedAt time.Time  `json:"started_at" gorm:"autoCreateTime"`
    ExpiresAt *time.Time `json:"expires_at"`
}

// From internal/model/user.go (current — AMEND per this plan)
type User struct {
    ID                    string     `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    // ... existing fields including SubscriptionTier, SubscriptionExpiresAt, Email, AppleUserID, GoogleUserID ...
    // ADD: PlanID string `json:"plan_id" gorm:"column:plan_id;type:uuid;not null;index"`
}

// PlanLimits map — DELETE entirely. Keep UnlimitedServers/UnlimitedDevices constants.
const (
    UnlimitedServers = -1
    UnlimitedDevices = -1
)
```
</interfaces>

<tasks>

<task type="auto">
  <id>03-01-T01</id>
  <name>Add Wave 0 test dependencies (testcontainers, datatypes) to go.mod</name>
  <files>server/api/go.mod, server/api/go.sum</files>
  <read_first>
    - server/api/go.mod (current go 1.25 + miniredis v2.37.0 + stripe-go v81 + gorm v1.30 + postgres driver v1.5.9)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §9 (testcontainers usage for migration tests)
    - .planning/phases/03-lava-top-plans-catalog/03-VALIDATION.md "Wave 0 Requirements" list
  </read_first>
  <action>
    From `server/api/` directory run:
      go get github.com/testcontainers/testcontainers-go@latest
      go get github.com/testcontainers/testcontainers-go/modules/postgres@latest
      go get gorm.io/datatypes@latest
      go mod tidy
    These three modules are required by Wave 0 of subsequent tasks: testcontainers-go is used in `migrations/migrations_test.go` (this plan, T05) to spin a real Postgres 16 for the migration smoke; the postgres module supplies the Postgres image helper; `gorm.io/datatypes` exposes `datatypes.JSON` used as the GORM type for the `lava_webhook_events.payload` jsonb column (T04). Do NOT bump or remove any existing dependency. After `go mod tidy`, run `go build ./...` from `server/api/` to confirm the module graph still resolves.
  </action>
  <acceptance_criteria>
    - `grep -E 'testcontainers-go|datatypes' server/api/go.mod` returns at least 3 matches (root testcontainers module, postgres submodule, gorm datatypes)
    - `go build ./...` from `server/api/` exits 0
    - `git diff server/api/go.mod | grep -c '^+	github.com/testcontainers/testcontainers-go'` returns at least 2 (one for root + one for postgres module)
    - `go.sum` contains `gorm.io/datatypes` entries
  </acceptance_criteria>
  <automated>cd server/api && go build ./...</automated>
  <done>go.mod lists testcontainers-go, testcontainers-go/modules/postgres, gorm.io/datatypes; `go build ./...` succeeds.</done>
</task>

<task type="auto">
  <id>03-01-T02</id>
  <name>Write migration 019_plans_catalog.sql (plans, plan_servers, plan_offers, users.plan_id, tier coercion, seeds)</name>
  <files>server/api/migrations/019_plans_catalog.sql</files>
  <read_first>
    - server/api/migrations/018_add_sso_columns.sql (Phase 2 pattern — BEGIN/COMMIT, IF NOT EXISTS, doc-comment block, partial unique indexes, CHECK constraint)
    - docs/ADR-007-lava-sso-rework.md §19.3 (canonical plans/plan_servers/plan_offers DDL + seed logic)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-07, D-08, D-09 (filename 019, Pro max_devices=3, 6 placeholder offers for Pro)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §8.7 (varchar(40) code), §15 row 1 (PAY-01 mapping)
  </read_first>
  <action>
    Create `server/api/migrations/019_plans_catalog.sql` with this verbatim content (matches D-08 overrides — Pro max_devices=3 not 5; 6 placeholder offers per D-09; tier coercion premium/ultimate→pro per D-08; wrapped in BEGIN/COMMIT per the Phase 2 migration pattern):

```sql
-- 019_plans_catalog.sql
--
-- Phase 3 (PAY-01): dynamic plans replace the hard-coded PlanLimits Go map.
-- Three new tables (plans, plan_servers, plan_offers), a new FK (users.plan_id),
-- legacy tier coercion (premium/ultimate -> pro — destruction-free per CONTEXT D-08,
-- zero paying users today), and seed rows so the existing 'free'/'pro' strings
-- still resolve via plans.code lookup.
--
-- Sources of truth:
--   - docs/ADR-007-lava-sso-rework.md §19.3 (DDL)
--   - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-07 (filename 019),
--     D-08 (Pro max_devices=3 not 5), D-09 (6 placeholder Pro offers).
--
-- Idempotency: wrapped in BEGIN/COMMIT; golang-migrate auto-rollbacks on failure.
-- All CREATE INDEX statements use IF NOT EXISTS so a partial re-run is safe.

BEGIN;

-- 1. plans table
CREATE TABLE plans (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code               VARCHAR(40)  NOT NULL UNIQUE,
    name               VARCHAR(100) NOT NULL,
    description        TEXT         NOT NULL DEFAULT '',
    max_devices        INT          NOT NULL,
    max_servers        INT          NOT NULL,
    speed_limit_mbps   INT          NOT NULL DEFAULT 0,
    is_active          BOOLEAN      NOT NULL DEFAULT TRUE,
    is_system          BOOLEAN      NOT NULL DEFAULT FALSE,
    sort_order         INT          NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Soft enum guardrail on code (regex-like CHECK).
ALTER TABLE plans
    ADD CONSTRAINT plans_code_format_check
        CHECK (code ~ '^[a-z0-9][a-z0-9_-]*$');

-- Enforce exactly-one-system-plan invariant.
CREATE UNIQUE INDEX IF NOT EXISTS idx_plans_one_system
    ON plans(is_system) WHERE is_system = TRUE;
CREATE INDEX IF NOT EXISTS idx_plans_active_sort
    ON plans(is_active, sort_order) WHERE is_active = TRUE;

-- 2. plan_servers M:N table
CREATE TABLE plan_servers (
    plan_id    UUID NOT NULL REFERENCES plans(id)        ON DELETE CASCADE,
    server_id  UUID NOT NULL REFERENCES vpn_servers(id)  ON DELETE CASCADE,
    PRIMARY KEY (plan_id, server_id)
);
CREATE INDEX IF NOT EXISTS idx_plan_servers_server ON plan_servers(server_id);

-- 3. plan_offers table
CREATE TABLE plan_offers (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id        UUID         NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    periodicity    VARCHAR(20)  NOT NULL,
    currency       VARCHAR(3)   NOT NULL,
    amount         NUMERIC(10,2) NOT NULL CHECK (amount >= 0),
    lava_offer_id  VARCHAR(64),
    is_active      BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Whitelist periodicity + currency at the DB level (defence in depth — handlers
-- ALSO validate, per RESEARCH §"Security Domain").
ALTER TABLE plan_offers
    ADD CONSTRAINT plan_offers_periodicity_check
        CHECK (periodicity IN ('ONE_TIME','MONTHLY','PERIOD_90_DAYS','PERIOD_180_DAYS','PERIOD_YEAR'));
ALTER TABLE plan_offers
    ADD CONSTRAINT plan_offers_currency_check
        CHECK (currency IN ('USD','EUR','RUB'));

-- One ACTIVE offer per (plan, periodicity, currency); grandfathered (is_active=false)
-- copies stay around for renewal webhooks (ADR §19.10).
CREATE UNIQUE INDEX IF NOT EXISTS idx_plan_offers_unique_active
    ON plan_offers(plan_id, periodicity, currency) WHERE is_active = TRUE;
CREATE UNIQUE INDEX IF NOT EXISTS idx_plan_offers_lava_offer_id
    ON plan_offers(lava_offer_id) WHERE lava_offer_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_plan_offers_plan_active
    ON plan_offers(plan_id) WHERE is_active = TRUE;

-- 4. Seed plans. D-08: Pro max_devices=3 (NOT 5 — overrides ADR §19.3 default).
INSERT INTO plans (code, name, description, max_devices, max_servers, speed_limit_mbps, is_system, sort_order)
VALUES
    ('free', 'Free', '', 1, 3, 50, TRUE, 0),
    ('pro',  'Pro',  '', 3, -1, 0, FALSE, 10);

-- 5. Coerce legacy premium/ultimate -> pro (D-08, destruction-free, zero paying users today).
UPDATE users         SET subscription_tier = 'pro' WHERE subscription_tier IN ('premium', 'ultimate');
UPDATE subscriptions SET plan              = 'pro' WHERE plan              IN ('premium', 'ultimate');

-- 6. Add users.plan_id FK, backfill, then NOT NULL.
ALTER TABLE users ADD COLUMN plan_id UUID REFERENCES plans(id) ON DELETE SET NULL;
UPDATE users u
   SET plan_id = p.id
  FROM plans p
 WHERE p.code = u.subscription_tier;
-- Defensive: any orphan row gets the system plan.
UPDATE users
   SET plan_id = (SELECT id FROM plans WHERE is_system = TRUE)
 WHERE plan_id IS NULL;
ALTER TABLE users ALTER COLUMN plan_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_plan_id ON users(plan_id);

-- 7. Seed plan_servers — Free gets 3 lowest-load active servers (matches the old
--    PlanLimits["free"].MaxServers=3 slicing); Pro gets every active server.
INSERT INTO plan_servers (plan_id, server_id)
SELECT (SELECT id FROM plans WHERE code = 'free'), v.id
  FROM vpn_servers v
 WHERE v.is_active = TRUE
 ORDER BY v.current_load ASC
 LIMIT 3;

INSERT INTO plan_servers (plan_id, server_id)
SELECT (SELECT id FROM plans WHERE code = 'pro'), v.id
  FROM vpn_servers v
 WHERE v.is_active = TRUE;

-- 8. D-09: seed 6 PLACEHOLDER plan_offers rows for Pro
--    ({MONTHLY, PERIOD_YEAR} × {USD, EUR, RUB}) with lava_offer_id=NULL.
--    Admin opens each in the UI dropdown picker post-deploy and selects matching
--    lava offer. /checkout returns 409 offer_not_configured until lava_offer_id is set.
INSERT INTO plan_offers (plan_id, periodicity, currency, amount, lava_offer_id)
SELECT id, 'MONTHLY',     'USD', 5.00,  NULL FROM plans WHERE code='pro' UNION ALL
SELECT id, 'PERIOD_YEAR', 'USD', 49.99, NULL FROM plans WHERE code='pro' UNION ALL
SELECT id, 'MONTHLY',     'EUR', 5.00,  NULL FROM plans WHERE code='pro' UNION ALL
SELECT id, 'PERIOD_YEAR', 'EUR', 49.99, NULL FROM plans WHERE code='pro' UNION ALL
SELECT id, 'MONTHLY',     'RUB', 499.00,NULL FROM plans WHERE code='pro' UNION ALL
SELECT id, 'PERIOD_YEAR', 'RUB', 4990.0,NULL FROM plans WHERE code='pro';

COMMIT;
```

Verify by running the migration locally if PG is up (`psql ... < 019_plans_catalog.sql`). Tested in T05 via testcontainers.
  </action>
  <acceptance_criteria>
    - File `server/api/migrations/019_plans_catalog.sql` exists
    - `grep -c "BEGIN;" server/api/migrations/019_plans_catalog.sql` returns 1
    - `grep -c "COMMIT;" server/api/migrations/019_plans_catalog.sql` returns 1
    - `grep "max_devices, max_servers, speed_limit_mbps, is_system, sort_order" server/api/migrations/019_plans_catalog.sql` finds one INSERT VALUES line
    - `grep "'pro',  'Pro',  '', 3, -1, 0, FALSE, 10" server/api/migrations/019_plans_catalog.sql` finds one match (Pro max_devices=3 per D-08)
    - `grep -c "SELECT id, " server/api/migrations/019_plans_catalog.sql` returns 6 (six placeholder offers per D-09)
    - `grep "ALTER TABLE users ALTER COLUMN plan_id SET NOT NULL" server/api/migrations/019_plans_catalog.sql` finds one match
    - `grep "UPDATE users.*subscription_tier = 'pro' WHERE subscription_tier IN ('premium', 'ultimate')" server/api/migrations/019_plans_catalog.sql` finds one match
  </acceptance_criteria>
  <automated>grep -c "CREATE TABLE plans" server/api/migrations/019_plans_catalog.sql</automated>
  <done>Migration 019 file matches ADR §19.3 + D-08/D-09 overrides; psql parse-only validates.</done>
</task>

<task type="auto">
  <id>03-01-T03</id>
  <name>Write migration 020_lava_payments.sql (invoices, lava_contracts, lava_webhook_events; drop subscriptions.stripe_id; add lava_contract_id; COALESCE UNIQUE)</name>
  <files>server/api/migrations/020_lava_payments.sql</files>
  <read_first>
    - server/api/migrations/019_plans_catalog.sql (the file you just wrote — plan_id/plan_offer_id FKs reference plans/plan_offers tables created there)
    - docs/ADR-007-lava-sso-rework.md §8.3 (invoices/lava_contracts/lava_webhook_events DDL) and §19.6 (invoices.plan_id + invoices.plan_offer_id amendment)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-10, D-11 (UNIQUE COALESCE; drop stripe_id; add lava_contract_id)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §1.5 ("subscription.cancelled has no timestamp" → COALESCE), §4.2 (CREATE UNIQUE INDEX vs inline), §15 row 1 (PAY-01 mapping)
  </read_first>
  <action>
    Create `server/api/migrations/020_lava_payments.sql` with this verbatim content. Note three deliberate choices from research:
    (a) `idx_lava_webhook_events_natural_key` uses `COALESCE((payload->>'timestamp'), (payload->>'cancelledAt'))` because `subscription.cancelled` events have no `timestamp` field per lava OpenAPI (RESEARCH §1.5).
    (b) The UNIQUE is declared as a separate `CREATE UNIQUE INDEX` (RESEARCH §4.2 — inline `UNIQUE (... (expr))` is not valid Postgres syntax for expressions).
    (c) `subscriptions.stripe_id` is dropped here (D-11); the Go code that referenced it is rewritten in T04 + T06.

```sql
-- 020_lava_payments.sql
--
-- Phase 3 (PAY-03..05, PAY-09, PAY-11): lava.top payment provider schema.
--
-- Three tables (invoices, lava_contracts, lava_webhook_events), plus a
-- schema patch on the existing subscriptions table (drop stripe_id, add
-- lava_contract_id). The invoices table carries BOTH the lava-side
-- offer UUID (offer_id — forensics) AND the internal FKs (plan_id,
-- plan_offer_id — joins/reporting) per ADR §19.6.
--
-- Sources of truth:
--   - docs/ADR-007-lava-sso-rework.md §8.3 (base DDL) + §19.6 (plan_id/plan_offer_id additions)
--   - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-10 (COALESCE timestamp/cancelledAt), D-11 (drop stripe_id)
--   - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §1.5 (subscription.cancelled has no timestamp), §4.2 (CREATE UNIQUE INDEX, not inline expression UNIQUE)

BEGIN;

-- 1. invoices — one row per /checkout call, status lifecycle pending->paid|failed|cancelled
CREATE TABLE invoices (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lava_invoice_id VARCHAR(64)  NOT NULL UNIQUE,
    offer_id        VARCHAR(64)  NOT NULL,            -- lava-side offer UUID (forensics)
    plan_id         UUID         REFERENCES plans(id),        -- ADR §19.6 amendment
    plan_offer_id   UUID         REFERENCES plan_offers(id),  -- ADR §19.6 amendment
    plan            VARCHAR(20)  NOT NULL,
    periodicity     VARCHAR(20)  NOT NULL,
    currency        VARCHAR(3)   NOT NULL,
    amount          NUMERIC(10,2) NOT NULL,
    status          VARCHAR(20)  NOT NULL,            -- pending|paid|failed|cancelled
    payment_url     TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_invoices_user_id ON invoices(user_id);
CREATE INDEX IF NOT EXISTS idx_invoices_status  ON invoices(status);
CREATE INDEX IF NOT EXISTS idx_invoices_plan_id ON invoices(plan_id);

-- 2. lava_contracts — one row per recurring contract (lava-side identity)
CREATE TABLE lava_contracts (
    id                  UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contract_id         VARCHAR(64)  NOT NULL UNIQUE,   -- lava {contractId}
    parent_contract_id  VARCHAR(64),                    -- populated on subscription.recurring.* events
    offer_id            VARCHAR(64)  NOT NULL,
    plan                VARCHAR(20)  NOT NULL,
    periodicity         VARCHAR(20)  NOT NULL,
    currency            VARCHAR(3)   NOT NULL,
    is_active           BOOLEAN      NOT NULL DEFAULT TRUE,
    started_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at          TIMESTAMPTZ,
    cancelled_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lava_contracts_user_id ON lava_contracts(user_id);
CREATE INDEX IF NOT EXISTS idx_lava_contracts_parent ON lava_contracts(parent_contract_id);

-- 3. lava_webhook_events — idempotency log
CREATE TABLE lava_webhook_events (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type   VARCHAR(64)  NOT NULL,
    contract_id  VARCHAR(64),
    invoice_id   VARCHAR(64),
    payload      JSONB        NOT NULL,
    received_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    error        TEXT
);

-- Idempotency UNIQUE: subscription.cancelled events have no `timestamp` field
-- in lava's webhook payload (only `cancelledAt`) — so we COALESCE both per
-- RESEARCH §1.5 + §4.2. Both `->>` operators produce text; no cast needed
-- but CONTEXT.md D-10 keeps `::text` as documentation — harmless no-op.
CREATE UNIQUE INDEX IF NOT EXISTS idx_lava_webhook_events_natural_key
    ON lava_webhook_events (
        event_type,
        contract_id,
        COALESCE((payload->>'timestamp')::text, (payload->>'cancelledAt')::text)
    );

-- 4. Patch subscriptions: drop legacy Stripe column (D-11), add lava_contract_id.
--    The Go code referencing stripe_id is rewritten in companion tasks (T04, T06).
ALTER TABLE subscriptions DROP COLUMN IF EXISTS stripe_id;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS lava_contract_id VARCHAR(64);
CREATE INDEX IF NOT EXISTS idx_subscriptions_lava_contract_id
    ON subscriptions(lava_contract_id);

COMMIT;
```
  </action>
  <acceptance_criteria>
    - File `server/api/migrations/020_lava_payments.sql` exists
    - `grep -c "CREATE TABLE invoices" server/api/migrations/020_lava_payments.sql` returns 1
    - `grep -c "CREATE TABLE lava_contracts" server/api/migrations/020_lava_payments.sql` returns 1
    - `grep -c "CREATE TABLE lava_webhook_events" server/api/migrations/020_lava_payments.sql` returns 1
    - `grep "COALESCE((payload->>'timestamp')::text, (payload->>'cancelledAt')::text)" server/api/migrations/020_lava_payments.sql` finds one match
    - `grep "ALTER TABLE subscriptions DROP COLUMN IF EXISTS stripe_id" server/api/migrations/020_lava_payments.sql` finds one match
    - `grep "lava_contract_id VARCHAR(64)" server/api/migrations/020_lava_payments.sql` finds one match (ADD COLUMN)
    - `grep "plan_id         UUID         REFERENCES plans(id)" server/api/migrations/020_lava_payments.sql` finds one match (invoices.plan_id ADR §19.6)
    - `grep "plan_offer_id   UUID         REFERENCES plan_offers(id)" server/api/migrations/020_lava_payments.sql` finds one match
    - `grep "parent_contract_id" server/api/migrations/020_lava_payments.sql` finds matches (column + index)
  </acceptance_criteria>
  <automated>grep -c "CREATE TABLE\|CREATE UNIQUE INDEX" server/api/migrations/020_lava_payments.sql</automated>
  <done>Migration 020 file matches ADR §8.3 + §19.6 + D-10 COALESCE + D-11 stripe_id drop.</done>
</task>

<task type="auto">
  <id>03-01-T04</id>
  <name>Create GORM models (plan.go, invoice.go, lava_contract.go, lava_webhook_event.go); rewrite subscription.go; amend user.go</name>
  <files>
    server/api/internal/model/plan.go,
    server/api/internal/model/invoice.go,
    server/api/internal/model/lava_contract.go,
    server/api/internal/model/lava_webhook_event.go,
    server/api/internal/model/subscription.go,
    server/api/internal/model/user.go
  </files>
  <read_first>
    - server/api/internal/model/subscription.go (CURRENT — has StripeID + PlanLimits map, both will be removed/changed)
    - server/api/internal/model/user.go (CURRENT — add PlanID field at end of struct, BEFORE CreatedAt)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §8.2, §8.3, §8.4, §8.5 (model shapes verbatim)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-01 (rewrite payment.go in-place), D-11 (drop stripe_id column; add lava_contract_id)
  </read_first>
  <action>
    Five model file edits — copy each block VERBATIM (struct tags are load-bearing for the migration index choices in T02/T03):

    **(a) New file `server/api/internal/model/plan.go`:**
```go
package model

import "time"

// Plan is an admin-defined entitlement bundle per ADR §19.2.
//
// Exactly one row has is_system=TRUE — enforced by idx_plans_one_system
// (migration 019). When a paid plan expires, the scheduler (D-26) flips
// users.plan_id back to that row.
type Plan struct {
	ID             string    `json:"id"              gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Code           string    `json:"code"            gorm:"type:varchar(40);uniqueIndex;not null"`
	Name           string    `json:"name"            gorm:"type:varchar(100);not null"`
	Description    string    `json:"description"     gorm:"type:text;default:''"`
	MaxDevices     int       `json:"max_devices"     gorm:"not null"`
	MaxServers     int       `json:"max_servers"     gorm:"not null"`
	SpeedLimitMbps int       `json:"speed_limit_mbps" gorm:"not null;default:0"`
	IsActive       bool      `json:"is_active"       gorm:"not null;default:true"`
	IsSystem       bool      `json:"is_system"       gorm:"not null;default:false"`
	SortOrder      int       `json:"sort_order"      gorm:"not null;default:0"`
	CreatedAt      time.Time `json:"created_at"      gorm:"autoCreateTime"`
	UpdatedAt      time.Time `json:"updated_at"      gorm:"autoUpdateTime"`
}

// PlanServer is the M:N join between plans and vpn_servers.
// Composite PK matches the migration's PRIMARY KEY (plan_id, server_id).
type PlanServer struct {
	PlanID   string `json:"plan_id"   gorm:"primaryKey;type:uuid"`
	ServerID string `json:"server_id" gorm:"primaryKey;type:uuid"`
}

// PlanOffer is a (plan, periodicity, currency) tuple bound to a lava_offer_id.
// Multiple offers per plan; multiple rows per (plan, periodicity, currency)
// allowed but only ONE with is_active=true (partial unique idx_plan_offers_unique_active).
type PlanOffer struct {
	ID          string    `json:"id"             gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	PlanID      string    `json:"plan_id"        gorm:"type:uuid;not null;index"`
	Periodicity string    `json:"periodicity"    gorm:"type:varchar(20);not null"`
	Currency    string    `json:"currency"       gorm:"type:varchar(3);not null"`
	Amount      float64   `json:"amount"         gorm:"type:numeric(10,2);not null"`
	LavaOfferID *string   `json:"lava_offer_id"  gorm:"column:lava_offer_id;type:varchar(64)"`
	IsActive    bool      `json:"is_active"      gorm:"not null;default:true"`
	CreatedAt   time.Time `json:"created_at"     gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at"     gorm:"autoUpdateTime"`
}
```

    **(b) New file `server/api/internal/model/invoice.go`:**
```go
package model

import "time"

// Invoice is one row per /checkout call. Status lifecycle:
// pending -> paid | failed | cancelled (set by webhook handler).
type Invoice struct {
	ID            string    `json:"id"              gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	UserID        string    `json:"user_id"         gorm:"type:uuid;not null;index"`
	LavaInvoiceID string    `json:"lava_invoice_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	OfferID       string    `json:"offer_id"        gorm:"type:varchar(64);not null"` // lava-side offer UUID
	PlanID        *string   `json:"plan_id"         gorm:"type:uuid;index"`           // ADR §19.6
	PlanOfferID   *string   `json:"plan_offer_id"   gorm:"type:uuid;index"`           // ADR §19.6
	Plan          string    `json:"plan"            gorm:"type:varchar(20);not null"`
	Periodicity   string    `json:"periodicity"     gorm:"type:varchar(20);not null"`
	Currency      string    `json:"currency"        gorm:"type:varchar(3);not null"`
	Amount        float64   `json:"amount"          gorm:"type:numeric(10,2);not null"`
	Status        string    `json:"status"          gorm:"type:varchar(20);not null"`
	PaymentURL    string    `json:"payment_url"     gorm:"type:text"`
	CreatedAt     time.Time `json:"created_at"      gorm:"autoCreateTime"`
	UpdatedAt     time.Time `json:"updated_at"      gorm:"autoUpdateTime"`
}
```

    **(c) New file `server/api/internal/model/lava_contract.go`:**
```go
package model

import "time"

// LavaContract mirrors the lava-side recurring contract. ContractID is the
// lava-side UUID (unique); ParentContractID is populated on renewal events
// (subscription.recurring.payment.* webhooks per RESEARCH §1.5).
type LavaContract struct {
	ID               string     `json:"id"                  gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	UserID           string     `json:"user_id"             gorm:"type:uuid;not null;index"`
	ContractID       string     `json:"contract_id"         gorm:"type:varchar(64);uniqueIndex;not null"`
	ParentContractID *string    `json:"parent_contract_id"  gorm:"column:parent_contract_id;type:varchar(64);index"`
	OfferID          string     `json:"offer_id"            gorm:"type:varchar(64);not null"`
	Plan             string     `json:"plan"                gorm:"type:varchar(20);not null"`
	Periodicity      string     `json:"periodicity"         gorm:"type:varchar(20);not null"`
	Currency         string     `json:"currency"            gorm:"type:varchar(3);not null"`
	IsActive         bool       `json:"is_active"           gorm:"not null;default:true"`
	StartedAt        time.Time  `json:"started_at"          gorm:"autoCreateTime"`
	ExpiresAt        *time.Time `json:"expires_at"`
	CancelledAt      *time.Time `json:"cancelled_at"`
	CreatedAt        time.Time  `json:"created_at"          gorm:"autoCreateTime"`
}
```

    **(d) New file `server/api/internal/model/lava_webhook_event.go`:**
```go
package model

import (
	"time"

	"gorm.io/datatypes"
)

// LavaWebhookEvent is the idempotency + forensics log for inbound
// lava.top webhooks. The natural key (event_type, contract_id,
// COALESCE(payload->>timestamp, payload->>cancelledAt)) is enforced
// by idx_lava_webhook_events_natural_key (migration 020).
type LavaWebhookEvent struct {
	ID          string         `json:"id"           gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	EventType   string         `json:"event_type"   gorm:"type:varchar(64);not null"`
	ContractID  *string        `json:"contract_id"  gorm:"type:varchar(64)"`
	InvoiceID   *string        `json:"invoice_id"   gorm:"type:varchar(64)"`
	Payload     datatypes.JSON `json:"payload"      gorm:"type:jsonb;not null"`
	ReceivedAt  time.Time      `json:"received_at"  gorm:"autoCreateTime"`
	ProcessedAt *time.Time     `json:"processed_at"`
	Error       *string        `json:"error"        gorm:"type:text"`
}
```

    **(e) REWRITE `server/api/internal/model/subscription.go`** (delete the PlanLimits map; replace StripeID with LavaContractID; keep UnlimitedServers/UnlimitedDevices sentinels):
```go
package model

import "time"

// UnlimitedServers / UnlimitedDevices are sentinel values for Plan.MaxServers /
// Plan.MaxDevices. Handlers reading plans.* check for these before applying
// a slice or cap. Lives here for backward import compatibility — handlers
// like connection.go, devices.go, servers.go reference model.UnlimitedDevices /
// model.UnlimitedServers directly.
const UnlimitedServers = -1
const UnlimitedDevices = -1

// Subscription is the canonical "current entitlement" record. Phase 3 drops
// stripe_id (migration 020) and adds lava_contract_id. The PlanLimits map
// is DELETED — limits now live in the plans table (queried via plan_repo).
type Subscription struct {
	ID             string     `json:"id"                gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	UserID         string     `json:"user_id"           gorm:"not null;index"`
	Plan           string     `json:"plan"              gorm:"not null;default:free"`
	LavaContractID *string    `json:"-"                 gorm:"column:lava_contract_id;type:varchar(64);index"`
	IsActive       bool       `json:"is_active"         gorm:"default:true"`
	StartedAt      time.Time  `json:"started_at"        gorm:"autoCreateTime"`
	ExpiresAt      *time.Time `json:"expires_at"`
}
```

    **(f) AMEND `server/api/internal/model/user.go`** — add `PlanID string` field. Locate the existing struct (after `AuthProvider` and BEFORE `CreatedAt`), insert this single line:
```go
	PlanID              string    `json:"plan_id" gorm:"column:plan_id;type:uuid;not null;index"`
```

    After the edit, run `cd server/api && go build ./...` — expect errors in `handler/payment.go` (references stripe-go), `handler/admin.go`, `handler/connection.go`, `handler/devices.go`, `handler/health.go`, `handler/servers.go`, `repository/subscription_repo.go`, `repository/subscription_repo_test.go`, `handler/payment_test.go`. Those are addressed in T05 (test file cleanup) and the other Wave 2/3 plans (03-03, 03-04, 03-05). For NOW, the model files alone must compile (model package only); after T05 + T06 the whole repo builds again.
  </action>
  <acceptance_criteria>
    - File `server/api/internal/model/plan.go` exists with `type Plan struct`, `type PlanServer struct`, `type PlanOffer struct`
    - `grep -c "^type Plan struct\|^type PlanServer struct\|^type PlanOffer struct" server/api/internal/model/plan.go` returns 3
    - File `server/api/internal/model/invoice.go` exists with `type Invoice struct`
    - File `server/api/internal/model/lava_contract.go` exists with `type LavaContract struct`
    - File `server/api/internal/model/lava_webhook_event.go` exists with `type LavaWebhookEvent struct`
    - `grep "datatypes.JSON" server/api/internal/model/lava_webhook_event.go` finds one match
    - `grep -c "PlanLimits" server/api/internal/model/subscription.go` returns 0 (map deleted)
    - `grep -c "StripeID" server/api/internal/model/subscription.go` returns 0
    - `grep "LavaContractID" server/api/internal/model/subscription.go` finds one match
    - `grep "UnlimitedServers\s*=\s*-1\|UnlimitedDevices\s*=\s*-1" server/api/internal/model/subscription.go` finds matches (constants preserved)
    - `grep "PlanID" server/api/internal/model/user.go` finds one match
    - `cd server/api && go build ./internal/model/...` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./internal/model/...</automated>
  <done>All 5 model files reflect the Phase 3 shape; model package compiles standalone.</done>
</task>

<task type="auto">
  <id>03-01-T05</id>
  <name>Resolve D-01/D-03 conflict — rewrite subscription_repo.go (drop StripeID); update subscription_repo_test.go; remove payment_test.go orphan Stripe assertions</name>
  <files>
    server/api/internal/repository/subscription_repo.go,
    server/api/internal/repository/subscription_repo_test.go,
    server/api/internal/handler/payment_test.go
  </files>
  <read_first>
    - server/api/internal/repository/subscription_repo.go (CURRENT — has FindSubscriptionByStripeID + Updates map writing "stripe_id")
    - server/api/internal/repository/subscription_repo_test.go (CURRENT — lines 52, 142, 144, 152, 159, 167, 205, 228, 240, 260, 261 reference stripe_id / StripeID)
    - server/api/internal/handler/payment_test.go (CURRENT — line 75 DDL, line 112 writes StripeID, lines 326, 439 test names)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §14 (D-01 ↔ D-03 hidden conflict — exact removal targets), Risk #6 (delete or stub payment_test.go before model rewrite breaks build)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-01 (rewrite payment.go in-place), D-03 (keep stripe-go in go.mod through Phase 3)
  </read_first>
  <action>
    Three file edits to unblock the build after T04 removes `model.Subscription.StripeID`:

    **(a) REWRITE `server/api/internal/repository/subscription_repo.go`** — delete `FindSubscriptionByStripeID` entirely; rewrite `CreateOrUpdateSubscription` to drop the "stripe_id" key from the Updates map; keep `FindSubscriptionByUserID`, `CreateSubscription`, `DeactivateSubscription` unchanged in signature:

```go
package repository

import (
	"errors"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// FindSubscriptionByUserID returns the most recent active subscription for a user.
func FindSubscriptionByUserID(db *gorm.DB, userID string) (*model.Subscription, error) {
	var sub model.Subscription
	result := db.Where("user_id = ? AND is_active = ?", userID, true).
		Order("started_at DESC").
		First(&sub)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &sub, nil
}

// CreateSubscription inserts a new subscription record.
func CreateSubscription(db *gorm.DB, sub *model.Subscription) error {
	return db.Create(sub).Error
}

// CreateOrUpdateSubscription upserts a subscription matched on user_id.
// If an active subscription for the user already exists it is updated in place;
// otherwise a new row is inserted.
//
// Phase 3 (D-11): writes lava_contract_id instead of the dropped stripe_id column.
func CreateOrUpdateSubscription(db *gorm.DB, sub *model.Subscription) error {
	var existing model.Subscription
	result := db.Where("user_id = ? AND is_active = ?", sub.UserID, true).
		Order("started_at DESC").
		First(&existing)

	if result.Error != nil {
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		}
		// No active subscription — insert a new one.
		return db.Create(sub).Error
	}

	// Update the existing row.
	return db.Model(&existing).Updates(map[string]interface{}{
		"plan":             sub.Plan,
		"lava_contract_id": sub.LavaContractID,
		"is_active":        sub.IsActive,
		"expires_at":       sub.ExpiresAt,
	}).Error
}

// DeactivateSubscription marks a subscription as inactive by its primary key.
func DeactivateSubscription(db *gorm.DB, subID string) error {
	result := db.Model(&model.Subscription{}).
		Where("id = ?", subID).
		Update("is_active", false)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
```

    **(b) UPDATE `server/api/internal/repository/subscription_repo_test.go`** — make the file build + reflect the new shape. The full update is:
    1. In the test-local CREATE TABLE string at line ~52, replace `stripe_id TEXT,` with `lava_contract_id TEXT,`.
    2. DELETE the entire `// ---- FindSubscriptionByStripeID ----` block (`TestFindSubscriptionByStripeID_NotFound`, `TestFindSubscriptionByStripeID_Found`) — these helpers no longer exist.
    3. In the remaining tests, replace every literal `StripeID: "sub_abc123"` / `"sub_new"` / `"sub_old"` with `LavaContractID: ptr("contract_abc123")` / `ptr("contract_new")` / `ptr("contract_old")`. Add a `func ptr[T any](v T) *T { return &v }` helper at the bottom of the file (or reuse if it exists in the same package — `subscription_repo_test.go` is the only test file mentioned to use it, so add it).
    4. In the assertion that previously read `found.StripeID != "sub_new"` (line 260-261) replace with:
       `if found.LavaContractID == nil || *found.LavaContractID != "contract_new" { t.Errorf("expected lava_contract_id=contract_new after update, got %v", found.LavaContractID) }`
    5. Rename the assertion function from anything mentioning "Stripe" to "LavaContract" (the test function names that previously contained `_Stripe_` become `_LavaContract_`).

    **(c) UPDATE `server/api/internal/handler/payment_test.go`** — per RESEARCH §14 / Risk #6, this file currently references `model.Subscription.StripeID` which T04 removed. Two-step minimum to unblock the build:
    1. In the test-local CREATE TABLE string at line ~75, replace `stripe_id TEXT,` with `lava_contract_id TEXT,`.
    2. In the `seedSubscription` helper around line ~112 that writes `StripeID: stripeID`, replace with `LavaContractID: &stripeID` (rename the parameter to `lavaContractID` for clarity but keep `stripeID` as a local string-pointer if it's less churn — pick the smaller diff).
    3. Tests like `TestCancelSubscription_NoStripeID` (line ~326) and `TestHandleSubscriptionDeleted_UnknownStripeID` (line ~439) test Stripe-only paths that will be DELETED in plan 03-05 when payment.go is rewritten. For THIS plan, add a `t.Skip("Stripe path deleted in 03-05 — see Phase 8 HARD-01 for full removal")` as the FIRST line of each Stripe-only test body (do NOT delete the test function bodies — D-03 keeps stripe-go in go.mod so the Stripe Go imports still resolve; we just bypass executing the assertions until 03-05 deletes the handlers). Two `t.Skip` calls land in this plan.

    Then run `cd server/api && go build ./...` to confirm it compiles. NOTE: at this point handler/payment.go still references stripe-go and is unchanged (D-01 rewrite lands in plan 03-05), so `go build ./...` should still succeed — only the StripeID FIELD references break. Run `cd server/api && go test ./internal/repository/...` and `cd server/api && go test ./internal/handler/ -run TestCancelSubscription` to confirm tests build and either pass or skip cleanly.
  </action>
  <acceptance_criteria>
    - `grep -c "FindSubscriptionByStripeID" server/api/internal/repository/subscription_repo.go` returns 0
    - `grep -c "stripe_id" server/api/internal/repository/subscription_repo.go` returns 0
    - `grep "lava_contract_id" server/api/internal/repository/subscription_repo.go` finds one match (in Updates map)
    - `grep -c "TestFindSubscriptionByStripeID" server/api/internal/repository/subscription_repo_test.go` returns 0
    - `grep -c "StripeID" server/api/internal/repository/subscription_repo_test.go` returns 0
    - `grep "LavaContractID" server/api/internal/repository/subscription_repo_test.go` finds matches
    - `grep -c "t.Skip" server/api/internal/handler/payment_test.go` returns at least 2
    - `grep -c "StripeID: stripeID" server/api/internal/handler/payment_test.go` returns 0 (replaced with pointer write)
    - `cd server/api && go build ./...` exits 0
    - `cd server/api && go test ./internal/repository/ -run TestCreateOrUpdateSubscription -count=1 -timeout=30s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./... && go test ./internal/repository/ -run "TestSubscription|TestCreateOrUpdate|TestFindSubscriptionByUserID" -count=1 -timeout=30s</automated>
  <done>Build is green after T04's model rewrite; subscription_repo tests pass against new LavaContractID column; payment_test.go Stripe-only tests skip cleanly.</done>
</task>

<task type="auto">
  <id>03-01-T06</id>
  <name>Write migrations_test.go (testcontainers Postgres harness verifying 019 + 020 land correctly — PAY-01 gate)</name>
  <files>server/api/migrations/migrations_test.go</files>
  <read_first>
    - server/api/migrations/019_plans_catalog.sql (just written in T02)
    - server/api/migrations/020_lava_payments.sql (just written in T03)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §9.2 (test skeleton verbatim)
    - .planning/phases/03-lava-top-plans-catalog/03-VALIDATION.md PAY-01 row (this is the test for the Per-Task Verification Map)
  </read_first>
  <action>
    Create `server/api/migrations/migrations_test.go` with a testcontainers-Postgres harness that boots Postgres 16, applies migrations 001..018 (replay all existing SQL files in lexicographic order), seeds three legacy users (premium/ultimate/free tiers), applies 019, asserts the assertions listed below, then applies 020 and asserts the rest. Use `testcontainers-go/modules/postgres` for the boot helper.

    File contents:

```go
//go:build !integration
// +build !integration

package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// migrationsDir is resolved from the file location at test time so the test
// runs correctly regardless of the working directory chosen by `go test`.
func migrationsDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}

// applyMigration reads a single .sql file and executes it. Postgres
// supports the multi-statement BEGIN; ... COMMIT; blocks we use directly.
func applyMigration(t *testing.T, db *sql.DB, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatalf("apply %s: %v", filepath.Base(path), err)
	}
}

func TestMigrations019_020(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode — skipping testcontainers")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Enable gen_random_uuid (Postgres 16 has pgcrypto out of the box, but the
	// extension is not auto-enabled).
	if _, err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		t.Fatalf("enable pgcrypto: %v", err)
	}

	// Apply 001..018 in lexicographic order.
	dir := migrationsDir(t)
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var sqlFiles []string
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		sqlFiles = append(sqlFiles, name)
	}
	sort.Strings(sqlFiles)
	for _, name := range sqlFiles {
		// We apply through 020 here so the final assertions see both 019 and 020 effects;
		// the per-stage assertions break this sequence below.
		if name == "019_plans_catalog.sql" || name == "020_lava_payments.sql" {
			continue // applied with assertions below
		}
		applyMigration(t, db, filepath.Join(dir, name))
	}

	// Seed legacy users for the coercion check (D-08).
	if _, err := db.Exec(`
		INSERT INTO users (id, full_name, subscription_tier) VALUES
		    (gen_random_uuid(), 'a', 'premium'),
		    (gen_random_uuid(), 'b', 'ultimate'),
		    (gen_random_uuid(), 'c', 'free')`); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	// Apply 019.
	applyMigration(t, db, filepath.Join(dir, "019_plans_catalog.sql"))

	// --- Assertions on 019 (PAY-01) ---
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE subscription_tier IN ('premium','ultimate')`).Scan(&n); err != nil {
		t.Fatalf("count legacy tiers: %v", err)
	}
	if n != 0 {
		t.Errorf("D-08: premium/ultimate must be coerced to pro; %d remain", n)
	}

	var proMaxDevices int
	if err := db.QueryRow(`SELECT max_devices FROM plans WHERE code = 'pro'`).Scan(&proMaxDevices); err != nil {
		t.Fatalf("query pro max_devices: %v", err)
	}
	if proMaxDevices != 3 {
		t.Errorf("D-08: Pro max_devices must be 3, got %d", proMaxDevices)
	}

	if err := db.QueryRow(`SELECT count(*) FROM plan_offers WHERE plan_id = (SELECT id FROM plans WHERE code='pro')`).Scan(&n); err != nil {
		t.Fatalf("count pro offers: %v", err)
	}
	if n != 6 {
		t.Errorf("D-09: 6 placeholder offers expected for Pro, got %d", n)
	}

	if err := db.QueryRow(`SELECT count(*) FROM users WHERE plan_id IS NULL`).Scan(&n); err != nil {
		t.Fatalf("count null plan_id: %v", err)
	}
	if n != 0 {
		t.Errorf("all users must have plan_id backfilled; %d remain NULL", n)
	}

	// Apply 020.
	applyMigration(t, db, filepath.Join(dir, "020_lava_payments.sql"))

	// --- Assertions on 020 (PAY-01 + D-11) ---
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM information_schema.columns
		WHERE table_name='subscriptions' AND column_name='stripe_id')`).Scan(&exists); err != nil {
		t.Fatalf("check stripe_id existence: %v", err)
	}
	if exists {
		t.Errorf("D-11: subscriptions.stripe_id must be dropped")
	}

	if err := db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM information_schema.columns
		WHERE table_name='subscriptions' AND column_name='lava_contract_id')`).Scan(&exists); err != nil {
		t.Fatalf("check lava_contract_id existence: %v", err)
	}
	if !exists {
		t.Errorf("D-11: subscriptions.lava_contract_id must be added")
	}

	// Sanity: all six new tables exist.
	for _, table := range []string{"plans", "plan_servers", "plan_offers", "invoices", "lava_contracts", "lava_webhook_events"} {
		if err := db.QueryRow(`SELECT EXISTS(
			SELECT 1 FROM information_schema.tables WHERE table_name = $1)`, table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s must exist after 020", table)
		}
	}

	// COALESCE expression index sanity — insert a subscription.cancelled row
	// (no `timestamp` field, only `cancelledAt`), then insert it AGAIN and
	// expect the unique constraint to reject it.
	payload1 := `{"cancelledAt":"2026-05-23T10:00:00Z","contractId":"contract-X"}`
	if _, err := db.Exec(`
		INSERT INTO lava_webhook_events (event_type, contract_id, payload)
		VALUES ('subscription.cancelled', 'contract-X', $1::jsonb)`, payload1); err != nil {
		t.Fatalf("first cancelled event insert: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO lava_webhook_events (event_type, contract_id, payload)
		VALUES ('subscription.cancelled', 'contract-X', $1::jsonb)`, payload1); err == nil {
		t.Errorf("D-10 COALESCE: duplicate subscription.cancelled (no timestamp) must violate unique")
	}
}
```

    Run `cd server/api && docker ps` to confirm Docker is up, then `cd server/api && go test ./migrations/ -run TestMigrations019_020 -count=1 -timeout=300s -v`. Expect ~30-60s on first run (Postgres image pull); subsequent runs are 5-10s.
  </action>
  <acceptance_criteria>
    - File `server/api/migrations/migrations_test.go` exists
    - `grep "tcpostgres.Run" server/api/migrations/migrations_test.go` finds one match
    - `grep -c "D-08" server/api/migrations/migrations_test.go` returns at least 2 (annotates assertions to decisions)
    - `grep "subscription.cancelled" server/api/migrations/migrations_test.go` finds one match (COALESCE test)
    - `cd server/api && go test ./migrations/ -run TestMigrations019_020 -count=1 -timeout=300s` exits 0
    - The test creates the migration test container, applies all migrations, and exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./migrations/ -run TestMigrations019_020 -count=1 -timeout=300s</automated>
  <done>migrations_test.go boots Postgres 16, applies 001-020, asserts D-08 (Pro=3 devices), D-09 (6 offers), D-10 (COALESCE uniqueness), D-11 (stripe_id dropped + lava_contract_id added). PAY-01 verified end-to-end.</done>
</task>

</tasks>

<verification>
After all 6 tasks land:
- `cd server/api && go build ./...` exits 0
- `cd server/api && go test ./internal/repository/ -count=1 -timeout=60s` exits 0 (existing subscription tests still pass against new shape)
- `cd server/api && go test ./migrations/ -run TestMigrations019_020 -count=1 -timeout=300s` exits 0 (PAY-01 end-to-end via testcontainers)
- `grep -rn 'FindSubscriptionByStripeID' server/api/internal/` returns 0 hits
- `grep -n 'StripeID' server/api/internal/model/subscription.go` returns 0 hits
- `grep -c 'plan_id\|plan_servers\|plan_offers\|lava_contract\|lava_webhook_events\|invoices' server/api/migrations/019_plans_catalog.sql server/api/migrations/020_lava_payments.sql` returns substantial coverage
</verification>

<must_haves>
truths:
  - "Migration 019 creates plans/plan_servers/plan_offers tables; backfills users.plan_id; seeds free + pro plans + 6 Pro placeholder offers; coerces premium/ultimate → pro destruction-free."
  - "Migration 020 creates invoices/lava_contracts/lava_webhook_events; drops subscriptions.stripe_id; adds subscriptions.lava_contract_id; expression UNIQUE on lava_webhook_events uses COALESCE(timestamp, cancelledAt)."
  - "Model package compiles standalone after PlanLimits map deletion and StripeID removal."
  - "Repository layer no longer references stripe_id column; payment_test.go orphan Stripe tests skip cleanly so the whole repo builds (D-01 ↔ D-03 conflict resolved)."
  - "PAY-01 verifiable via testcontainers Postgres: tests apply 019 + 020 sequentially and assert Pro=3 devices, 6 placeholder offers, stripe_id dropped, lava_contract_id added, COALESCE UNIQUE works for subscription.cancelled."
artifacts:
  - path: "server/api/migrations/019_plans_catalog.sql"
    provides: "Plans catalog schema + seed + users.plan_id FK"
    contains: "CREATE TABLE plans"
  - path: "server/api/migrations/020_lava_payments.sql"
    provides: "Lava payments schema + COALESCE expression unique index"
    contains: "COALESCE((payload->>'timestamp')"
  - path: "server/api/migrations/migrations_test.go"
    provides: "PAY-01 verification harness"
    contains: "TestMigrations019_020"
  - path: "server/api/internal/model/plan.go"
    provides: "Plan/PlanServer/PlanOffer GORM models"
    contains: "type Plan struct"
  - path: "server/api/internal/model/subscription.go"
    provides: "Rewritten Subscription with LavaContractID; PlanLimits map deleted"
    contains: "LavaContractID"
  - path: "server/api/internal/repository/subscription_repo.go"
    provides: "Stripe-free subscription repository"
    contains: "lava_contract_id"
key_links:
  - from: "server/api/internal/model/user.go"
    to: "server/api/migrations/019_plans_catalog.sql"
    via: "PlanID string column (FK to plans.id)"
    pattern: "PlanID\\s+string.*plan_id"
  - from: "server/api/migrations/020_lava_payments.sql"
    to: "server/api/migrations/019_plans_catalog.sql"
    via: "invoices.plan_id / invoices.plan_offer_id FKs to plans / plan_offers"
    pattern: "REFERENCES plans\\(id\\)"
</must_haves>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| Admin API → DB | Future plan-CRUD endpoints write into these tables; this plan ONLY creates the schema. |
| Webhook handler → DB | Future webhook handler writes to lava_webhook_events; idempotency UNIQUE is the security primitive. |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-01 | Tampering | Migration 019 INSERT INTO plans | mitigate | `is_system` is hardcoded in the seed UPDATE; no API path can set is_system=true (migration-only invariant per D-32 §4). The partial unique idx_plans_one_system enforces "exactly one system plan" at the DB layer — admin abuse cannot create a second system plan. |
| T-03-02 | Tampering | plans.code column | mitigate | `plans_code_format_check` CHECK constraint regex `^[a-z0-9][a-z0-9_-]*$` rejects injection-shaped values at the DB layer. Defence in depth — handler validation lives in plan 03-08. |
| T-03-03 | Repudiation | lava_webhook_events | mitigate | All inbound webhooks logged to `payload` jsonb BEFORE processing — preserves forensic record even if processing fails (PAY-05). Idempotency UNIQUE prevents replay (PAY-04). |
| T-03-04 | Elevation of Privilege | users.plan_id backfill | mitigate | Backfill UPDATE uses `WHERE p.code = u.subscription_tier` (not client input). Any orphan row gets the system plan, never a paid plan — fail-safe default. |
| T-03-05 | Tampering | subscriptions.stripe_id drop | accept | D-03 keeps stripe-go in go.mod; orphan test files reference removed field, mitigated by T05 skip-stubs + payment_test.go cleanup. No production code path references the column after T05. |
| T-03-06 | Information disclosure | lava_webhook_events.payload | accept | The jsonb column will store webhook payloads including buyer email + amount; this is unavoidable for replay / forensics (Phase 7 ADMIN-06). Defence in depth: only admins read this table; future zap encoder redactor (Phase 8 HARD-10) catches accidental log leaks. |
| T-03-07 | Tampering | plan_offers.amount NUMERIC(10,2) | mitigate | `CHECK (amount >= 0)` at DB layer rejects negative-price exploit attempts. Type pinning to NUMERIC prevents float-precision tampering. |

ASVS scoping per D-31: this plan is migration/schema, classified L1 (no payment code paths execute yet); the L2 scoping applies to plans 03-02 (lava client) onward.
</threat_model>

<success_criteria>
1. `cd server/api && go build ./...` exits 0.
2. `cd server/api && go test ./migrations/ -run TestMigrations019_020 -count=1 -timeout=300s` exits 0 (testcontainers Postgres 16 — PAY-01 verified).
3. `cd server/api && go test ./internal/repository/ -count=1 -timeout=60s` exits 0 (existing subscription_repo tests pass against new LavaContractID shape).
4. `grep -rn 'PlanLimits' server/api/internal/model/` returns 0 hits (map deleted; constants UnlimitedServers/UnlimitedDevices still present).
5. `grep -rn 'FindSubscriptionByStripeID' server/api/internal/` returns 0 hits.
6. `grep -c 'StripeID' server/api/internal/model/subscription.go` returns 0.
</success_criteria>

<output>
After completion the executor commits each task atomically (T01..T06 = 6 commits, all prefixed `docs(03):` for this plan-write step, then in execution mode prefixed `feat(03-01):` or `chore(03-01):`). The plan file itself is committed once by the planner using:
`docs(03): plan migrations-models-stripe-cleanup`.
</output>
