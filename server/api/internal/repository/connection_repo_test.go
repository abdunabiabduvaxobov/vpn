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
	"gorm.io/gorm/logger"
)

// newTestDB opens an in-memory SQLite database with the tables required for
// connection tests.  SQLite is used so tests run without a real PostgreSQL
// instance.
//
// We create the schema via raw DDL rather than AutoMigrate because the GORM
// models use PostgreSQL-specific defaults (gen_random_uuid()) that SQLite does
// not understand.  The columns and constraints are equivalent.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}

	ddl := `
		CREATE TABLE IF NOT EXISTS vpn_servers (
			id TEXT PRIMARY KEY,
			hostname TEXT NOT NULL UNIQUE,
			ip_address TEXT NOT NULL,
			region TEXT NOT NULL,
			city TEXT NOT NULL,
			country TEXT NOT NULL,
			country_code TEXT NOT NULL,
			protocol TEXT NOT NULL DEFAULT 'vless-reality',
			capacity INTEGER NOT NULL DEFAULT 500,
			current_load INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			is_draining INTEGER NOT NULL DEFAULT 0,
			last_seen_at DATETIME,
			reality_public_key TEXT,
			reality_short_id TEXT,
			ws_enabled INTEGER NOT NULL DEFAULT 0,
			ws_host TEXT,
			ws_path TEXT DEFAULT '/ws',
			awg_public_key TEXT,
			awg_endpoint TEXT,
			awg_params TEXT,
			created_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS connections (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			server_id TEXT NOT NULL,
			connected_at DATETIME,
			disconnected_at DATETIME,
			bytes_up INTEGER NOT NULL DEFAULT 0,
			bytes_down INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'connected',
			last_heartbeat_at DATETIME
		);
	`
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatalf("failed to create test tables: %v", err)
	}

	return db
}

// seedServer inserts a minimal active VPNServer and returns its ID.
// The ID is pre-generated in Go so the test works with SQLite (no gen_random_uuid()).
func seedServer(t *testing.T, db *gorm.DB) string {
	t.Helper()

	srv := model.VPNServer{
		ID:          uuid.NewString(),
		Hostname:    "test-01",
		IPAddress:   "1.2.3.4",
		Region:      "test",
		City:        "TestCity",
		Country:     "Testland",
		CountryCode: "TT",
		Protocol:    "vless-reality",
		IsActive:    true,
	}
	if err := db.Create(&srv).Error; err != nil {
		t.Fatalf("failed to seed server: %v", err)
	}
	return srv.ID
}

// --- CreateConnection ---

func TestCreateConnection_InsertsRow(t *testing.T) {
	db := newTestDB(t)
	serverID := seedServer(t, db)

	conn := &model.Connection{
		UserID:   "user-abc",
		ServerID: serverID,
	}

	if err := repository.CreateConnection(context.Background(), db, conn); err != nil {
		t.Fatalf("CreateConnection returned error: %v", err)
	}

	if conn.ID == "" {
		t.Fatal("expected connection ID to be set after creation")
	}
}

func TestCreateConnection_SetsConnectedAt(t *testing.T) {
	db := newTestDB(t)
	serverID := seedServer(t, db)

	before := time.Now().Add(-time.Second)
	conn := &model.Connection{UserID: "user-abc", ServerID: serverID}

	if err := repository.CreateConnection(context.Background(), db, conn); err != nil {
		t.Fatalf("CreateConnection returned error: %v", err)
	}

	if conn.ConnectedAt.Before(before) {
		t.Errorf("ConnectedAt %v is before test start %v", conn.ConnectedAt, before)
	}
}

// --- CountActiveConnections ---

func TestCountActiveConnections_OnlyCountsActiveRows(t *testing.T) {
	db := newTestDB(t)
	serverID := seedServer(t, db)

	const userID = "user-count"

	// Insert two active and one disconnected connection.
	for i := 0; i < 2; i++ {
		conn := &model.Connection{UserID: userID, ServerID: serverID}
		if err := repository.CreateConnection(context.Background(), db, conn); err != nil {
			t.Fatalf("CreateConnection: %v", err)
		}
	}

	// Insert and immediately disconnect a third.
	disconnected := &model.Connection{UserID: userID, ServerID: serverID}
	if err := repository.CreateConnection(context.Background(), db, disconnected); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	if err := repository.DisconnectConnection(context.Background(), db, disconnected.ID, 0, 0); err != nil {
		t.Fatalf("DisconnectConnection: %v", err)
	}

	count, err := repository.CountActiveConnections(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("CountActiveConnections returned error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 active connections, got %d", count)
	}
}

func TestCountActiveConnections_ReturnsZeroForNewUser(t *testing.T) {
	db := newTestDB(t)

	count, err := repository.CountActiveConnections(context.Background(), db, "no-connections-user")
	if err != nil {
		t.Fatalf("CountActiveConnections returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for user with no connections, got %d", count)
	}
}

func TestCountActiveConnections_DoesNotCountOtherUsers(t *testing.T) {
	db := newTestDB(t)
	serverID := seedServer(t, db)

	// User A has 3 active connections.
	for i := 0; i < 3; i++ {
		conn := &model.Connection{UserID: "user-A", ServerID: serverID}
		if err := repository.CreateConnection(context.Background(), db, conn); err != nil {
			t.Fatalf("CreateConnection: %v", err)
		}
	}

	count, err := repository.CountActiveConnections(context.Background(), db, "user-B")
	if err != nil {
		t.Fatalf("CountActiveConnections returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for user-B, got %d", count)
	}
}

// --- DisconnectConnection ---

func TestDisconnectConnection_SetsDisconnectedAt(t *testing.T) {
	db := newTestDB(t)
	serverID := seedServer(t, db)

	conn := &model.Connection{UserID: "user-dc", ServerID: serverID}
	if err := repository.CreateConnection(context.Background(), db, conn); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	if err := repository.DisconnectConnection(context.Background(), db, conn.ID, 1024, 2048); err != nil {
		t.Fatalf("DisconnectConnection returned error: %v", err)
	}

	// Read back to verify.
	fetched, err := repository.FindConnectionByID(context.Background(), db, conn.ID)
	if err != nil {
		t.Fatalf("FindConnectionByID: %v", err)
	}
	if fetched.DisconnectedAt == nil {
		t.Fatal("expected DisconnectedAt to be set, got nil")
	}
	if fetched.BytesUp != 1024 {
		t.Errorf("expected BytesUp 1024, got %d", fetched.BytesUp)
	}
	if fetched.BytesDown != 2048 {
		t.Errorf("expected BytesDown 2048, got %d", fetched.BytesDown)
	}
}

func TestDisconnectConnection_AlreadyDisconnectedReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	serverID := seedServer(t, db)

	conn := &model.Connection{UserID: "user-dd", ServerID: serverID}
	if err := repository.CreateConnection(context.Background(), db, conn); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	if err := repository.DisconnectConnection(context.Background(), db, conn.ID, 0, 0); err != nil {
		t.Fatalf("first DisconnectConnection: %v", err)
	}

	// Second call must return ErrNotFound (the WHERE filters on disconnected_at IS NULL).
	err := repository.DisconnectConnection(context.Background(), db, conn.ID, 0, 0)
	if err == nil {
		t.Fatal("expected ErrNotFound on second disconnect, got nil")
	}
	if err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDisconnectConnection_UnknownIDReturnsNotFound(t *testing.T) {
	db := newTestDB(t)

	err := repository.DisconnectConnection(context.Background(), db, "nonexistent-id", 0, 0)
	if err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound for unknown ID, got %v", err)
	}
}

// --- ListActiveConnectionsByUser ---

func TestListActiveConnectionsByUser_ReturnsOnlyActive(t *testing.T) {
	db := newTestDB(t)
	serverID := seedServer(t, db)

	const userID = "user-list"

	active1 := &model.Connection{UserID: userID, ServerID: serverID}
	active2 := &model.Connection{UserID: userID, ServerID: serverID}
	gone := &model.Connection{UserID: userID, ServerID: serverID}

	for _, c := range []*model.Connection{active1, active2, gone} {
		if err := repository.CreateConnection(context.Background(), db, c); err != nil {
			t.Fatalf("CreateConnection: %v", err)
		}
	}
	if err := repository.DisconnectConnection(context.Background(), db, gone.ID, 0, 0); err != nil {
		t.Fatalf("DisconnectConnection: %v", err)
	}

	list, err := repository.ListActiveConnectionsByUser(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("ListActiveConnectionsByUser returned error: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 active connections, got %d", len(list))
	}
}

func TestListActiveConnectionsByUser_EmptyForNewUser(t *testing.T) {
	db := newTestDB(t)

	list, err := repository.ListActiveConnectionsByUser(context.Background(), db, "ghost-user")
	if err != nil {
		t.Fatalf("ListActiveConnectionsByUser returned error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty slice, got %d elements", len(list))
	}
}

// --- CleanupStaleConnections ---

func TestCleanupStaleConnections_MarksOldConnections(t *testing.T) {
	db := newTestDB(t)
	serverID := seedServer(t, db)

	// Create a connection and then manually backdate its connected_at so it
	// appears stale.
	conn := &model.Connection{UserID: "user-stale", ServerID: serverID}
	if err := repository.CreateConnection(context.Background(), db, conn); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	staleTime := time.Now().Add(-3 * time.Hour)
	if err := db.Model(&model.Connection{}).Where("id = ?", conn.ID).
		Updates(map[string]interface{}{
			"connected_at":      staleTime,
			"last_heartbeat_at": staleTime,
		}).Error; err != nil {
		t.Fatalf("failed to backdate connected_at and last_heartbeat_at: %v", err)
	}

	// Connections older than 2 hours should be cleaned up.
	affected, err := repository.CleanupStaleConnections(context.Background(), db, 2*time.Hour)
	if err != nil {
		t.Fatalf("CleanupStaleConnections returned error: %v", err)
	}
	if affected != 1 {
		t.Errorf("expected 1 affected row, got %d", affected)
	}

	// Verify the row is now disconnected.
	fetched, err := repository.FindConnectionByID(context.Background(), db, conn.ID)
	if err != nil {
		t.Fatalf("FindConnectionByID: %v", err)
	}
	if fetched.DisconnectedAt == nil {
		t.Fatal("expected DisconnectedAt to be set after cleanup")
	}
}

func TestCleanupStaleConnections_DoesNotTouchRecentConnections(t *testing.T) {
	db := newTestDB(t)
	serverID := seedServer(t, db)

	conn := &model.Connection{UserID: "user-fresh", ServerID: serverID}
	if err := repository.CreateConnection(context.Background(), db, conn); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	affected, err := repository.CleanupStaleConnections(context.Background(), db, 2*time.Hour)
	if err != nil {
		t.Fatalf("CleanupStaleConnections returned error: %v", err)
	}
	if affected != 0 {
		t.Errorf("expected 0 affected for a fresh connection, got %d", affected)
	}

	fetched, err := repository.FindConnectionByID(context.Background(), db, conn.ID)
	if err != nil {
		t.Fatalf("FindConnectionByID: %v", err)
	}
	if fetched.DisconnectedAt != nil {
		t.Fatal("expected DisconnectedAt to remain nil for a fresh connection")
	}
}

// --- FindConnectionByID ---

func TestFindConnectionByID_ReturnsNotFoundForMissingID(t *testing.T) {
	db := newTestDB(t)

	_, err := repository.FindConnectionByID(context.Background(), db, "does-not-exist")
	if err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFindConnectionByID_ReturnsExistingRecord(t *testing.T) {
	db := newTestDB(t)
	serverID := seedServer(t, db)

	conn := &model.Connection{UserID: "user-find", ServerID: serverID}
	if err := repository.CreateConnection(context.Background(), db, conn); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fetched, err := repository.FindConnectionByID(context.Background(), db, conn.ID)
	if err != nil {
		t.Fatalf("FindConnectionByID returned error: %v", err)
	}
	if fetched.ID != conn.ID {
		t.Errorf("expected ID %s, got %s", conn.ID, fetched.ID)
	}
	if fetched.UserID != "user-find" {
		t.Errorf("expected UserID user-find, got %s", fetched.UserID)
	}
}
