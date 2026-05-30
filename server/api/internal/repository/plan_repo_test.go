package repository_test

import (
	"context"
	"testing"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func ptrStr(v string) *string { return &v }

// setupPlanRepoDB creates the minimum schema for plan_repo tests.
// SQLite does NOT support `gen_random_uuid()` — tests pass explicit UUIDs.
func setupPlanRepoDB(t *testing.T) *gorm.DB {
	t.Helper()
	// SQLite :memory: databases are per-connection by default. Pin the pool to
	// a single connection so db.Transaction(...) cannot grab a fresh,
	// empty-database connection (matches the auth_test.go race-test pattern).
	// Each test still gets its own isolated DB because gorm.Open creates a
	// fresh handle per call.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	stmts := []string{
		`CREATE TABLE plans (
			id TEXT PRIMARY KEY,
			code TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			max_devices INTEGER NOT NULL,
			max_servers INTEGER NOT NULL,
			speed_limit_mbps INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			is_system INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE plan_servers (
			plan_id TEXT NOT NULL,
			server_id TEXT NOT NULL,
			PRIMARY KEY (plan_id, server_id)
		)`,
		`CREATE TABLE plan_offers (
			id TEXT PRIMARY KEY,
			plan_id TEXT NOT NULL,
			periodicity TEXT NOT NULL,
			currency TEXT NOT NULL,
			amount REAL NOT NULL,
			lava_offer_id TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email_hash TEXT,
			password_hash TEXT,
			full_name TEXT NOT NULL DEFAULT '',
			subscription_tier TEXT NOT NULL DEFAULT 'free',
			subscription_expires_at TIMESTAMP,
			role TEXT NOT NULL DEFAULT 'user',
			telegram_user_id INTEGER,
			telegram_linked_at TIMESTAMP,
			telegram_username TEXT,
			telegram_first_name TEXT,
			apple_user_id TEXT,
			google_user_id TEXT,
			email TEXT,
			email_verified INTEGER NOT NULL DEFAULT 0,
			email_is_private_relay INTEGER NOT NULL DEFAULT 0,
			auth_provider TEXT NOT NULL DEFAULT 'guest',
			plan_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		// `id` has a DEFAULT so SQLite generates a UUID-like value when GORM
		// omits the column (the model's `default:gen_random_uuid()` tag tells
		// GORM to skip the field when it's the zero value, expecting the DB to
		// fill it — Postgres has gen_random_uuid(), SQLite needs this DEFAULT).
		// Without this, the row stores id=NULL and GORM First() with
		// `ORDER BY started_at DESC, subscriptions.id` cannot reliably find it
		// inside a db.Transaction, leading to an empty `existing.ID` that
		// triggers "WHERE conditions required" on a subsequent Updates(...) call.
		`CREATE TABLE subscriptions (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			user_id TEXT NOT NULL,
			plan TEXT NOT NULL DEFAULT 'free',
			lava_contract_id TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP
		)`,
		`CREATE TABLE vpn_servers (
			id TEXT PRIMARY KEY,
			hostname TEXT NOT NULL,
			ip_address TEXT NOT NULL DEFAULT '',
			region TEXT NOT NULL DEFAULT '',
			city TEXT NOT NULL DEFAULT '',
			country_code TEXT NOT NULL DEFAULT '',
			country TEXT NOT NULL DEFAULT '',
			protocol TEXT NOT NULL DEFAULT 'vless',
			capacity INTEGER NOT NULL DEFAULT 500,
			is_active INTEGER NOT NULL DEFAULT 1,
			current_load INTEGER NOT NULL DEFAULT 0,
			reality_public_key TEXT NOT NULL DEFAULT '',
			reality_short_id TEXT NOT NULL DEFAULT '',
			ws_enabled INTEGER NOT NULL DEFAULT 0,
			ws_host TEXT NOT NULL DEFAULT '',
			ws_path TEXT NOT NULL DEFAULT '',
			awg_public_key TEXT,
			awg_endpoint TEXT,
			awg_params TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	return db
}

func seedTwoPlans(t *testing.T, db *gorm.DB) (free, pro model.Plan) {
	t.Helper()
	free = model.Plan{ID: uuid.NewString(), Code: "free", Name: "Free", MaxDevices: 1, MaxServers: 3, SpeedLimitMbps: 50, IsActive: true, IsSystem: true, SortOrder: 0}
	pro = model.Plan{ID: uuid.NewString(), Code: "pro", Name: "Pro", MaxDevices: 3, MaxServers: -1, SpeedLimitMbps: 0, IsActive: true, IsSystem: false, SortOrder: 10}
	if err := db.Create(&free).Error; err != nil {
		t.Fatalf("seed free: %v", err)
	}
	if err := db.Create(&pro).Error; err != nil {
		t.Fatalf("seed pro: %v", err)
	}
	return
}

func TestFindPlanByID_FoundAndNotFound(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	got, err := repository.FindPlanByID(context.Background(), db, pro.ID)
	if err != nil {
		t.Fatalf("FindPlanByID: %v", err)
	}
	if got.Code != "pro" {
		t.Errorf("expected pro, got %s", got.Code)
	}
	if _, err := repository.FindPlanByID(context.Background(), db, "missing"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFindPlanByCode_FoundAndNotFound(t *testing.T) {
	db := setupPlanRepoDB(t)
	seedTwoPlans(t, db)
	got, err := repository.FindPlanByCode(context.Background(), db, "pro")
	if err != nil || got.Code != "pro" {
		t.Errorf("expected pro, got %+v err=%v", got, err)
	}
	if _, err := repository.FindPlanByCode(context.Background(), db, "ultimate"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFindSystemPlanID_HappyPath(t *testing.T) {
	db := setupPlanRepoDB(t)
	free, _ := seedTwoPlans(t, db)
	id, err := repository.FindSystemPlanID(context.Background(), db)
	if err != nil {
		t.Fatalf("FindSystemPlanID: %v", err)
	}
	if id != free.ID {
		t.Errorf("expected free plan id %s, got %s", free.ID, id)
	}
}

func TestListActivePlans_FiltersInactive(t *testing.T) {
	db := setupPlanRepoDB(t)
	free, pro := seedTwoPlans(t, db)
	// Deactivate pro.
	if err := db.Model(&model.Plan{}).Where("id = ?", pro.ID).Update("is_active", false).Error; err != nil {
		t.Fatalf("deactivate pro: %v", err)
	}
	plans, err := repository.ListActivePlans(context.Background(), db)
	if err != nil {
		t.Fatalf("ListActivePlans: %v", err)
	}
	if len(plans) != 1 || plans[0].ID != free.ID {
		t.Errorf("expected [free], got %+v", plans)
	}
}

func TestListServersForPlan_FiltersByPlanAndActive(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	s1 := model.VPNServer{ID: uuid.NewString(), Hostname: "s1", CountryCode: "NL", IsActive: true, CurrentLoad: 10}
	s2 := model.VPNServer{ID: uuid.NewString(), Hostname: "s2", CountryCode: "DE", IsActive: true, CurrentLoad: 5}
	// IsActive=false is the Go zero value for bool — GORM omits it from INSERT,
	// so the SQLite DDL default (1) wins. Insert active then UPDATE to false,
	// matching the pattern used in subscription_repo_test.go::TestFindSubscriptionByUserID_SkipsInactiveSub.
	s3 := model.VPNServer{ID: uuid.NewString(), Hostname: "s3", CountryCode: "US", IsActive: true, CurrentLoad: 1}
	for _, s := range []model.VPNServer{s1, s2, s3} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("seed server: %v", err)
		}
	}
	if err := db.Model(&model.VPNServer{}).Where("id = ?", s3.ID).Update("is_active", false).Error; err != nil {
		t.Fatalf("deactivate s3: %v", err)
	}
	// Plan pro gets all three (including inactive s3 in plan_servers).
	for _, s := range []model.VPNServer{s1, s2, s3} {
		if err := db.Create(&model.PlanServer{PlanID: pro.ID, ServerID: s.ID}).Error; err != nil {
			t.Fatalf("seed plan_servers: %v", err)
		}
	}
	servers, err := repository.ListServersForPlan(context.Background(), db, pro.ID)
	if err != nil {
		t.Fatalf("ListServersForPlan: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 active servers, got %d", len(servers))
	}
	// Ordered by current_load ASC.
	if servers[0].Hostname != "s2" || servers[1].Hostname != "s1" {
		t.Errorf("expected order [s2, s1], got [%s, %s]", servers[0].Hostname, servers[1].Hostname)
	}
}

func TestIsServerAllowedForPlan_TrueFalse(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	sid := uuid.NewString()
	if err := db.Create(&model.VPNServer{ID: sid, Hostname: "s", IsActive: true}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Create(&model.PlanServer{PlanID: pro.ID, ServerID: sid}).Error; err != nil {
		t.Fatalf("link: %v", err)
	}
	ok, err := repository.IsServerAllowedForPlan(context.Background(), db, pro.ID, sid)
	if err != nil || !ok {
		t.Errorf("expected true, got %v err=%v", ok, err)
	}
	ok, err = repository.IsServerAllowedForPlan(context.Background(), db, pro.ID, "non-existent")
	if err != nil || ok {
		t.Errorf("expected false, got %v err=%v", ok, err)
	}
}

func TestFindActiveOffer_ReturnsActiveOnly(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	active := model.PlanOffer{ID: uuid.NewString(), PlanID: pro.ID, Periodicity: "MONTHLY", Currency: "USD", Amount: 5.0, IsActive: true, LavaOfferID: ptrStr("off-new")}
	// IsActive=false is the Go zero value for bool — GORM omits it from INSERT,
	// so the DDL default (1) wins. Seed as active, then UPDATE to false.
	inactive := model.PlanOffer{ID: uuid.NewString(), PlanID: pro.ID, Periodicity: "MONTHLY", Currency: "USD", Amount: 3.99, IsActive: true, LavaOfferID: ptrStr("off-old")}
	if err := db.Create(&active).Error; err != nil {
		t.Fatalf("seed active: %v", err)
	}
	if err := db.Create(&inactive).Error; err != nil {
		t.Fatalf("seed inactive: %v", err)
	}
	if err := db.Model(&model.PlanOffer{}).Where("id = ?", inactive.ID).Update("is_active", false).Error; err != nil {
		t.Fatalf("deactivate inactive: %v", err)
	}
	got, err := repository.FindActiveOffer(context.Background(), db, pro.ID, "MONTHLY", "USD")
	if err != nil {
		t.Fatalf("FindActiveOffer: %v", err)
	}
	if got.ID != active.ID {
		t.Errorf("expected active offer, got %+v", got)
	}
}

func TestFindOfferByLavaOfferID_GrandfatheredInactive(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	// Inactive offer with lava_offer_id — must still resolve (renewal webhook).
	// Seed as active then UPDATE to false (GORM omits IsActive=false zero value).
	off := model.PlanOffer{ID: uuid.NewString(), PlanID: pro.ID, Periodicity: "MONTHLY", Currency: "USD", Amount: 3.99, IsActive: true, LavaOfferID: ptrStr("lava-old")}
	if err := db.Create(&off).Error; err != nil {
		t.Fatalf("seed offer: %v", err)
	}
	if err := db.Model(&model.PlanOffer{}).Where("id = ?", off.ID).Update("is_active", false).Error; err != nil {
		t.Fatalf("deactivate offer: %v", err)
	}
	got, err := repository.FindOfferByLavaOfferID(context.Background(), db, "lava-old")
	if err != nil {
		t.Fatalf("FindOfferByLavaOfferID: %v", err)
	}
	if got.ID != off.ID {
		t.Errorf("PAY-08 grandfathering: expected to resolve inactive offer for renewals, got %+v", got)
	}
}

func TestSetUserPlan_UpdatesUserAndUpsertsSubscription(t *testing.T) {
	db := setupPlanRepoDB(t)
	free, pro := seedTwoPlans(t, db)
	uid := uuid.NewString()
	if err := db.Create(&model.User{ID: uid, FullName: "u", SubscriptionTier: "free", PlanID: free.ID, AuthProvider: "google"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	exp := time.Now().Add(31 * 24 * time.Hour)
	contractID := "contract-abc"
	if err := repository.SetUserPlan(context.Background(), db, uid, pro.ID, &contractID, &exp); err != nil {
		t.Fatalf("SetUserPlan: %v", err)
	}

	// User row should reflect new plan.
	var u model.User
	if err := db.First(&u, "id = ?", uid).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u.PlanID != pro.ID || u.SubscriptionTier != "pro" {
		t.Errorf("expected plan_id=%s tier=pro, got plan_id=%s tier=%s", pro.ID, u.PlanID, u.SubscriptionTier)
	}
	if u.SubscriptionExpiresAt == nil {
		t.Errorf("PAY-09: subscription_expires_at must be populated")
	}

	// A subscriptions row should be inserted (no prior active row).
	var sub model.Subscription
	if err := db.Where("user_id = ? AND is_active = ?", uid, true).First(&sub).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if sub.Plan != "pro" {
		t.Errorf("expected plan=pro, got %s", sub.Plan)
	}
	if sub.LavaContractID == nil || *sub.LavaContractID != "contract-abc" {
		t.Errorf("expected lava_contract_id=contract-abc, got %v", sub.LavaContractID)
	}

	// Call again with a NEW expires_at — must update the existing row, not insert.
	newExp := exp.Add(30 * 24 * time.Hour)
	if err := repository.SetUserPlan(context.Background(), db, uid, pro.ID, &contractID, &newExp); err != nil {
		t.Fatalf("SetUserPlan (renewal): %v", err)
	}
	var subs []model.Subscription
	if err := db.Where("user_id = ?", uid).Find(&subs).Error; err != nil {
		t.Fatalf("count subs: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("renewal must update in place, got %d sub rows", len(subs))
	}
}

func TestSoftDeletePlan_RefusesSystemPlan(t *testing.T) {
	db := setupPlanRepoDB(t)
	free, pro := seedTwoPlans(t, db)
	if err := repository.SoftDeletePlan(context.Background(), db, free.ID); err != repository.ErrSystemPlan {
		t.Errorf("expected ErrSystemPlan, got %v", err)
	}
	// Non-system plan deletes fine.
	if err := repository.SoftDeletePlan(context.Background(), db, pro.ID); err != nil {
		t.Errorf("expected success, got %v", err)
	}
	var p model.Plan
	if err := db.First(&p, "id = ?", pro.ID).Error; err != nil {
		t.Fatalf("reload pro: %v", err)
	}
	if p.IsActive {
		t.Errorf("expected pro is_active=false after soft delete")
	}
}

func TestUpdatePlan_StripsImmutableFields(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	updates := map[string]interface{}{
		"code":        "newcode",
		"is_system":   true,
		"id":          "tampered",
		"name":        "New Pro",
		"max_devices": 10,
	}
	got, err := repository.UpdatePlan(context.Background(), db, pro.ID, updates)
	if err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	if got.Code != "pro" {
		t.Errorf("code must remain immutable, got %s", got.Code)
	}
	if got.IsSystem {
		t.Errorf("is_system must remain false")
	}
	if got.Name != "New Pro" || got.MaxDevices != 10 {
		t.Errorf("mutable fields not updated: %+v", got)
	}
}

func TestReplaceOffer_DeactivatesOldInsertsNewInOneTx(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	old := model.PlanOffer{ID: uuid.NewString(), PlanID: pro.ID, Periodicity: "MONTHLY", Currency: "USD", Amount: 3.99, IsActive: true, LavaOfferID: ptrStr("off-1")}
	if err := db.Create(&old).Error; err != nil {
		t.Fatalf("seed old: %v", err)
	}
	newOffer := &model.PlanOffer{ID: uuid.NewString(), PlanID: pro.ID, Periodicity: "MONTHLY", Currency: "USD", Amount: 5.0, LavaOfferID: ptrStr("off-2")}
	saved, err := repository.ReplaceOffer(context.Background(), db, old.ID, newOffer)
	if err != nil {
		t.Fatalf("ReplaceOffer: %v", err)
	}
	if !saved.IsActive {
		t.Errorf("new offer must be active")
	}
	// Old must be inactive now.
	var oldReloaded model.PlanOffer
	if err := db.First(&oldReloaded, "id = ?", old.ID).Error; err != nil {
		t.Fatalf("reload old: %v", err)
	}
	if oldReloaded.IsActive {
		t.Errorf("old offer must be deactivated after replace")
	}
}

func TestReplacePlanServers_AtomicReplacement(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	s1, s2, s3 := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{s1, s2, s3} {
		_ = db.Create(&model.VPNServer{ID: id, Hostname: id[:6], IsActive: true}).Error
	}
	if err := repository.ReplacePlanServers(context.Background(), db, pro.ID, []string{s1, s2}); err != nil {
		t.Fatalf("replace initial: %v", err)
	}
	if err := repository.ReplacePlanServers(context.Background(), db, pro.ID, []string{s2, s3}); err != nil {
		t.Fatalf("replace new: %v", err)
	}
	var rows []model.PlanServer
	if err := db.Where("plan_id = ?", pro.ID).Find(&rows).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 pairings, got %d", len(rows))
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.ServerID] = true
	}
	if !got[s2] || !got[s3] || got[s1] {
		t.Errorf("expected {s2, s3}, got %+v", got)
	}
}

func TestAddPlanServer_IdempotentOnReinsert(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	sid := uuid.NewString()
	_ = db.Create(&model.VPNServer{ID: sid, Hostname: "s", IsActive: true}).Error
	if err := repository.AddPlanServer(context.Background(), db, pro.ID, sid); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := repository.AddPlanServer(context.Background(), db, pro.ID, sid); err != nil {
		t.Errorf("second add must be idempotent, got %v", err)
	}
	var n int64
	_ = db.Model(&model.PlanServer{}).Where("plan_id = ? AND server_id = ?", pro.ID, sid).Count(&n).Error
	if n != 1 {
		t.Errorf("expected exactly 1 pairing, got %d", n)
	}
}

func TestRemovePlanServer_ReturnsErrNotFoundWhenMissing(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	if err := repository.RemovePlanServer(context.Background(), db, pro.ID, "nope"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
