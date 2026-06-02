package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

var errNilDB = fmt.Errorf("database connection is nil")

// ListUsers returns a paginated slice of users and the total matching count.
//
// HARD-06 (S2-3): the search filter is now PREFIX-only on indexed columns to
// kill the unbounded `ILIKE %x%` full-table scan that an admin-supplied string
// could trigger. We match `full_name ILIKE 'search%'` (anchored prefix — the
// idx_users_full_name index can serve it) and, only when the input parses as a
// full email address, an exact `email_hash = sha256hex(search)` equality (the
// existing zero-knowledge email lookup path). The old cast-id-to-text ILIKE
// branch is dropped entirely: a text cast over the id column can never use an
// index, and a full UUID paste is better served by AdminGetUser. The handler
// rejects len<3 searches before reaching here, so the prefix is always >=3 chars.
//
// page and limit must both be >= 1; the caller is responsible for validation.
func ListUsers(ctx context.Context, db *gorm.DB, page, limit int, search string) ([]model.User, int64, error) {
	if db == nil {
		return nil, 0, errNilDB
	}
	query := db.WithContext(ctx).Model(&model.User{})

	if search != "" {
		// Anchored prefix on the indexed full_name column — NO leading '%'.
		prefix := search + "%"
		if looksLikeEmail(search) {
			// Exact email match via the zero-knowledge hash. sha256hex matches
			// how email_hash is written at sign-in (handler/auth.go).
			emailHash := fmt.Sprintf("%x", sha256.Sum256([]byte(search)))
			query = query.Where("full_name ILIKE ? OR email_hash = ?", prefix, emailHash)
		} else {
			query = query.Where("full_name ILIKE ?", prefix)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("counting users: %w", err)
	}

	offset := (page - 1) * limit
	var users []model.User
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, fmt.Errorf("listing users: %w", err)
	}

	return users, total, nil
}

// looksLikeEmail reports whether the search input should be treated as a full
// email address (and thus matched exactly against email_hash). It mirrors the
// loose validation the auth login path uses: contains '@', plausible length.
// We deliberately keep this loose — a false positive only adds a harmless
// indexed equality clause; it never widens the scan.
func looksLikeEmail(s string) bool {
	return strings.Contains(s, "@") && len(s) >= 5 && len(s) <= 255
}

// UpdateUser applies an arbitrary set of column updates to a single user row.
// Only columns present in updates are modified; this prevents accidental zero-value overwrites.
// Returns ErrNotFound when no row matches userID.
func UpdateUser(ctx context.Context, db *gorm.DB, userID string, updates map[string]interface{}) error {
	if db == nil {
		return errNilDB
	}
	result := db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("updating user %s: %w", userID, result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateServer inserts a new VPN server record.
// Returns ErrDuplicate when the hostname already exists.
func CreateServer(ctx context.Context, db *gorm.DB, server *model.VPNServer) error {
	if db == nil {
		return errNilDB
	}
	result := db.WithContext(ctx).Create(server)
	if result.Error != nil {
		if isDuplicateError(result.Error) {
			return ErrDuplicate
		}
		return fmt.Errorf("creating server: %w", result.Error)
	}
	return nil
}

// UpdateServer applies an arbitrary set of column updates to a single VPN server row.
// Returns ErrNotFound when no row matches serverID.
func UpdateServer(ctx context.Context, db *gorm.DB, serverID string, updates map[string]interface{}) error {
	if db == nil {
		return errNilDB
	}
	result := db.WithContext(ctx).Model(&model.VPNServer{}).Where("id = ?", serverID).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("updating server %s: %w", serverID, result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteServer performs a soft delete by setting is_active = false.
// Returns ErrNotFound when no row matches serverID.
func DeleteServer(ctx context.Context, db *gorm.DB, serverID string) error {
	if db == nil {
		return errNilDB
	}
	result := db.WithContext(ctx).Model(&model.VPNServer{}).Where("id = ?", serverID).Update("is_active", false)
	if result.Error != nil {
		return fmt.Errorf("soft-deleting server %s: %w", serverID, result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAllServers returns every VPN server row, including inactive ones, ordered by hostname.
// This is the admin view; the public ListActiveServers only returns active servers.
func ListAllServers(ctx context.Context, db *gorm.DB) ([]model.VPNServer, error) {
	if db == nil {
		return nil, errNilDB
	}
	var servers []model.VPNServer
	if err := db.WithContext(ctx).Order("hostname ASC").Find(&servers).Error; err != nil {
		return nil, fmt.Errorf("listing all servers: %w", err)
	}
	return servers, nil
}

// GetGlobalStats returns dashboard-level aggregate counts.
// Keys in the returned map: total_users, active_subscriptions, server_count, active_server_count.
func GetGlobalStats(ctx context.Context, db *gorm.DB) (map[string]interface{}, error) {
	if db == nil {
		return nil, errNilDB
	}
	// Thread the request ctx onto the connection once; all four Count
	// queries below reuse the same context-bound session.
	db = db.WithContext(ctx)

	var totalUsers int64
	if err := db.Model(&model.User{}).Count(&totalUsers).Error; err != nil {
		return nil, fmt.Errorf("counting users: %w", err)
	}

	var activeSubscriptions int64
	if err := db.Model(&model.Subscription{}).
		Where("is_active = ?", true).
		Count(&activeSubscriptions).Error; err != nil {
		return nil, fmt.Errorf("counting active subscriptions: %w", err)
	}

	var serverCount int64
	if err := db.Model(&model.VPNServer{}).Count(&serverCount).Error; err != nil {
		return nil, fmt.Errorf("counting servers: %w", err)
	}

	var activeServerCount int64
	if err := db.Model(&model.VPNServer{}).
		Where("is_active = ?", true).
		Count(&activeServerCount).Error; err != nil {
		return nil, fmt.Errorf("counting active servers: %w", err)
	}

	return map[string]interface{}{
		"total_users":          totalUsers,
		"active_subscriptions": activeSubscriptions,
		"server_count":         serverCount,
		"active_server_count":  activeServerCount,
	}, nil
}

// GetDashboardKPIs returns the GetGlobalStats aggregate counts PLUS the
// ADMIN-01 live KPIs the operator dashboard's KPI bar shows:
//
//	paid_users          users on a non-system plan whose subscription has not lapsed
//	active_connections  connections currently up (heartbeat within the last 2 minutes)
//	signups_today/week/month  new users created in the trailing 1/7/30 days
//	churn_30d           lava_contracts cancelled in the last 30 days
//	failed_payments_30d lava_webhook_events of a *failed* type in the last 30 days
//
// The four GetGlobalStats keys (total_users, active_subscriptions, server_count,
// active_server_count) are preserved unchanged — no regression.
//
// Time bounds are computed in Go and passed as bind parameters so the same
// queries run on both Postgres (production) and SQLite (handler tests) without
// dialect-specific `now() - interval` literals. Every query threads ctx
// (PERF-07) via the single db.WithContext(ctx) session below.
func GetDashboardKPIs(ctx context.Context, db *gorm.DB) (map[string]interface{}, error) {
	if db == nil {
		return nil, errNilDB
	}

	// Start from the existing global stats so the four legacy keys are
	// guaranteed present and unchanged.
	stats, err := GetGlobalStats(ctx, db)
	if err != nil {
		return nil, err
	}

	db = db.WithContext(ctx)
	now := time.Now().UTC()
	heartbeatCutoff := now.Add(-2 * time.Minute)
	dayAgo := now.AddDate(0, 0, -1)
	weekAgo := now.AddDate(0, 0, -7)
	monthAgo := now.AddDate(0, 0, -30)

	// paid_users: on a non-system plan AND not lapsed. The system plan code is
	// resolved via a correlated subquery against plans.is_system so we never
	// hardcode the free-tier code.
	var paidUsers int64
	if err := db.Model(&model.User{}).
		Where("subscription_tier != (SELECT code FROM plans WHERE is_system = ? LIMIT 1)", true).
		Where("subscription_expires_at IS NULL OR subscription_expires_at > ?", now).
		Count(&paidUsers).Error; err != nil {
		return nil, fmt.Errorf("counting paid users: %w", err)
	}

	// active_connections: still up and heartbeating within the last 2 minutes.
	var activeConnections int64
	if err := db.Model(&model.Connection{}).
		Where("disconnected_at IS NULL AND last_heartbeat_at > ?", heartbeatCutoff).
		Count(&activeConnections).Error; err != nil {
		return nil, fmt.Errorf("counting active connections: %w", err)
	}

	// signups_today / week / month: trailing windows on users.created_at.
	var signupsToday, signupsWeek, signupsMonth int64
	if err := db.Model(&model.User{}).Where("created_at > ?", dayAgo).Count(&signupsToday).Error; err != nil {
		return nil, fmt.Errorf("counting signups today: %w", err)
	}
	if err := db.Model(&model.User{}).Where("created_at > ?", weekAgo).Count(&signupsWeek).Error; err != nil {
		return nil, fmt.Errorf("counting signups week: %w", err)
	}
	if err := db.Model(&model.User{}).Where("created_at > ?", monthAgo).Count(&signupsMonth).Error; err != nil {
		return nil, fmt.Errorf("counting signups month: %w", err)
	}

	// churn_30d: lava_contracts cancelled in the last 30 days (A2 — no new column).
	var churn30d int64
	if err := db.Table("lava_contracts").
		Where("cancelled_at > ?", monthAgo).
		Count(&churn30d).Error; err != nil {
		return nil, fmt.Errorf("counting 30d churn: %w", err)
	}

	// failed_payments_30d: webhook events of a failed type in the last 30 days.
	var failedPayments30d int64
	if err := db.Table("lava_webhook_events").
		Where("event_type LIKE ? AND received_at > ?", "%failed%", monthAgo).
		Count(&failedPayments30d).Error; err != nil {
		return nil, fmt.Errorf("counting 30d failed payments: %w", err)
	}

	stats["paid_users"] = paidUsers
	stats["active_connections"] = activeConnections
	stats["signups_today"] = signupsToday
	stats["signups_week"] = signupsWeek
	stats["signups_month"] = signupsMonth
	stats["churn_30d"] = churn30d
	stats["failed_payments_30d"] = failedPayments30d

	return stats, nil
}

// GetMRR returns an estimated monthly recurring revenue for the given currency,
// summed over the active paid users. Each paid user contributes the amount of
// the active offer for their plan in that currency: a MONTHLY offer contributes
// its full amount; a PERIOD_YEAR offer contributes amount/12 (annualised to a
// monthly figure). Users whose plan has no matching active offer in the
// requested currency contribute nothing.
//
// Returns (0, nil) when there are no paid users — an empty book is not an error.
// The whole expression is one aggregate query so the dashboard's 60s poll
// collapses to a single round-trip (and is cached 5 min in Redis by the caller,
// T-07-05). currency is a bound parameter, never string-concatenated (T-07-07).
func GetMRR(ctx context.Context, db *gorm.DB, currency string) (float64, error) {
	if db == nil {
		return 0, errNilDB
	}

	now := time.Now().UTC()

	// Join users → their plan's active offer for the requested currency.
	// MONTHLY contributes amount, PERIOD_YEAR contributes amount/12; any other
	// periodicity contributes 0. COALESCE keeps the SUM defined (0) when no
	// rows match. Parameterised CASE bounds + currency filter — no concatenation.
	var mrr float64
	row := db.WithContext(ctx).
		Table("users").
		Select(`COALESCE(SUM(
			CASE
				WHEN plan_offers.periodicity = ? THEN plan_offers.amount
				WHEN plan_offers.periodicity = ? THEN plan_offers.amount / 12.0
				ELSE 0
			END), 0)`, "MONTHLY", "PERIOD_YEAR").
		Joins("JOIN plans ON plans.id = users.plan_id AND plans.is_system = ?", false).
		Joins("JOIN plan_offers ON plan_offers.plan_id = users.plan_id AND plan_offers.currency = ? AND plan_offers.is_active = ?", currency, true).
		Where("users.subscription_expires_at IS NULL OR users.subscription_expires_at > ?", now).
		Row()
	if err := row.Scan(&mrr); err != nil {
		return 0, fmt.Errorf("computing MRR for %s: %w", currency, err)
	}
	return mrr, nil
}

// TimeseriesBucket is a single day in a dashboard timeseries. The date
// is formatted as YYYY-MM-DD in UTC so the frontend can plot without
// further parsing, and the count is whatever the caller asked for
// (signups, connections, etc.).
type TimeseriesBucket struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// GetTimeseries returns per-day signup and connection counts for the
// last `days` calendar days (UTC), padded with zero-count entries so
// the frontend always receives a contiguous series. The fixed window
// keeps query time bounded and the resulting JSON small.
func GetTimeseries(ctx context.Context, db *gorm.DB, days int) (signups, connections []TimeseriesBucket, err error) {
	if db == nil {
		return nil, nil, errNilDB
	}
	// Thread the request ctx onto the connection once; the signups and
	// connections aggregate queries below reuse the same context-bound session.
	db = db.WithContext(ctx)
	if days <= 0 || days > 180 {
		days = 30
	}

	// Build the zero-filled skeleton up front. We key by YYYY-MM-DD so
	// that Postgres's date_trunc results slot straight in.
	//
	// startDay must be truncated to midnight UTC. If we kept the
	// current time-of-day the query would discard rows created earlier
	// than that on the earliest bucket's date, silently under-counting
	// the oldest day in the window.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startDay := today.AddDate(0, 0, -(days - 1))
	signupMap := make(map[string]int64, days)
	connectMap := make(map[string]int64, days)
	orderedDays := make([]string, 0, days)
	for i := 0; i < days; i++ {
		day := startDay.AddDate(0, 0, i).Format("2006-01-02")
		signupMap[day] = 0
		connectMap[day] = 0
		orderedDays = append(orderedDays, day)
	}

	// Signups — users.created_at grouped by day.
	type row struct {
		Day   string
		Count int64
	}
	var signupRows []row
	if err := db.Model(&model.User{}).
		Select("TO_CHAR(DATE_TRUNC('day', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day, COUNT(*) AS count").
		Where("created_at >= ?", startDay).
		Group("day").
		Scan(&signupRows).Error; err != nil {
		return nil, nil, fmt.Errorf("querying signups timeseries: %w", err)
	}
	for _, r := range signupRows {
		if _, ok := signupMap[r.Day]; ok {
			signupMap[r.Day] = r.Count
		}
	}

	// Connections — count every connection row that started within the
	// window, regardless of whether it's still active. This matches the
	// "new connections per day" intuition the dashboard card will show.
	var connectRows []row
	if err := db.Model(&model.Connection{}).
		Select("TO_CHAR(DATE_TRUNC('day', connected_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day, COUNT(*) AS count").
		Where("connected_at >= ?", startDay).
		Group("day").
		Scan(&connectRows).Error; err != nil {
		return nil, nil, fmt.Errorf("querying connections timeseries: %w", err)
	}
	for _, r := range connectRows {
		if _, ok := connectMap[r.Day]; ok {
			connectMap[r.Day] = r.Count
		}
	}

	signups = make([]TimeseriesBucket, 0, days)
	connections = make([]TimeseriesBucket, 0, days)
	for _, day := range orderedDays {
		signups = append(signups, TimeseriesBucket{Date: day, Count: signupMap[day]})
		connections = append(connections, TimeseriesBucket{Date: day, Count: connectMap[day]})
	}
	return signups, connections, nil
}

// BytesBucket is a per-day bandwidth count. Emitted by GetBytesTimeseries
// to drive the dashboard's traffic chart. Up/down are stored separately
// because charts typically plot them as two stacked series.
type BytesBucket struct {
	Date      string `json:"date"`
	BytesUp   int64  `json:"bytes_up"`
	BytesDown int64  `json:"bytes_down"`
}

// GetBytesTimeseries returns per-day bytes_up and bytes_down totals for
// the last `days` days. The query SUMs over the `connections` table
// grouped by the day of `connected_at` — so a long-running connection
// counts entirely on the day it *started*, not the day bytes were
// actually moved. Good enough for capacity-planning trend lines; if
// you ever need high-precision accounting, log incremental deltas.
func GetBytesTimeseries(ctx context.Context, db *gorm.DB, days int) ([]BytesBucket, error) {
	if db == nil {
		return nil, errNilDB
	}
	if days <= 0 || days > 180 {
		days = 30
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startDay := today.AddDate(0, 0, -(days - 1))

	type row struct {
		Day       string
		BytesUp   int64
		BytesDown int64
	}
	var rows []row
	if err := db.WithContext(ctx).Model(&model.Connection{}).
		Select(
			"TO_CHAR(DATE_TRUNC('day', connected_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day, " +
				"COALESCE(SUM(bytes_up), 0) AS bytes_up, " +
				"COALESCE(SUM(bytes_down), 0) AS bytes_down",
		).
		Where("connected_at >= ?", startDay).
		Group("day").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("querying bytes timeseries: %w", err)
	}

	index := make(map[string]row, len(rows))
	for _, r := range rows {
		index[r.Day] = r
	}

	out := make([]BytesBucket, 0, days)
	for i := 0; i < days; i++ {
		day := startDay.AddDate(0, 0, i).Format("2006-01-02")
		r := index[day]
		out = append(out, BytesBucket{
			Date:      day,
			BytesUp:   r.BytesUp,
			BytesDown: r.BytesDown,
		})
	}
	return out, nil
}

// PlatformCount pairs a device platform string ("android", "ios", ...)
// with the number of devices currently bound to that platform across
// the whole user base.
type PlatformCount struct {
	Platform string `json:"platform"`
	Count    int64  `json:"count"`
}

// GetPlatformBreakdown returns one row per distinct platform in the
// devices table with the number of devices on that platform. The
// devices table has at most one row per physical device (share-code
// redemption reassigns user_id in place), so this is also the count
// of active physical devices by platform.
//
// Empty-string platforms are reported as "unknown" in the output so
// the UI does not need to special-case missing data.
func GetPlatformBreakdown(ctx context.Context, db *gorm.DB) ([]PlatformCount, error) {
	if db == nil {
		return nil, errNilDB
	}
	type row struct {
		Platform string
		Count    int64
	}
	var rows []row
	if err := db.WithContext(ctx).Model(&model.Device{}).
		Select("platform, COUNT(*) AS count").
		Group("platform").
		Order("count DESC").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("querying platform breakdown: %w", err)
	}
	out := make([]PlatformCount, 0, len(rows))
	for _, r := range rows {
		name := r.Platform
		if name == "" {
			name = "unknown"
		}
		out = append(out, PlatformCount{Platform: name, Count: r.Count})
	}
	return out, nil
}

// TierCount is the free/premium/ultimate distribution row.
type TierCount struct {
	Tier  string `json:"tier"`
	Count int64  `json:"count"`
}

// GetTierBreakdown returns the number of users on each subscription
// tier. Zero-fills missing tiers so the UI always receives a row for
// each of {free, premium, ultimate} regardless of whether the tier
// currently has any users — lets the donut chart render a stable
// legend.
func GetTierBreakdown(ctx context.Context, db *gorm.DB) ([]TierCount, error) {
	if db == nil {
		return nil, errNilDB
	}
	type row struct {
		Tier  string
		Count int64
	}
	var rows []row
	if err := db.WithContext(ctx).Model(&model.User{}).
		Select("subscription_tier AS tier, COUNT(*) AS count").
		Group("subscription_tier").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("querying tier breakdown: %w", err)
	}
	// Zero-fill canonical tiers in a fixed order.
	indexed := map[string]int64{}
	for _, r := range rows {
		indexed[r.Tier] = r.Count
	}
	canonical := []string{"free", "premium", "ultimate"}
	out := make([]TierCount, 0, len(canonical))
	for _, t := range canonical {
		out = append(out, TierCount{Tier: t, Count: indexed[t]})
	}
	return out, nil
}

// ServerUsage is one row of the "top N servers by connection count"
// analytics. Joins vpn_servers so the UI can render city/country
// without a second round-trip.
type ServerUsage struct {
	ServerID        string `json:"server_id"`
	Hostname        string `json:"hostname"`
	City            string `json:"city"`
	Country         string `json:"country"`
	CountryCode     string `json:"country_code"`
	ConnectionCount int64  `json:"connection_count"`
}

// GetTopServers returns the `limit` servers that handled the most
// connections in the last `days` days, newest-most-active first.
// Uses a plain INNER JOIN so servers with zero recent connections
// are excluded — the panel shows these as an empty-state instead.
func GetTopServers(ctx context.Context, db *gorm.DB, days, limit int) ([]ServerUsage, error) {
	if db == nil {
		return nil, errNilDB
	}
	if days <= 0 || days > 180 {
		days = 30
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startDay := today.AddDate(0, 0, -(days - 1))

	var out []ServerUsage
	if err := db.WithContext(ctx).Table("connections").
		Select(
			"vpn_servers.id AS server_id, " +
				"vpn_servers.hostname AS hostname, " +
				"vpn_servers.city AS city, " +
				"vpn_servers.country AS country, " +
				"vpn_servers.country_code AS country_code, " +
				"COUNT(connections.id) AS connection_count",
		).
		Joins("INNER JOIN vpn_servers ON vpn_servers.id = connections.server_id").
		Where("connections.connected_at >= ?", startDay).
		Group("vpn_servers.id, vpn_servers.hostname, vpn_servers.city, vpn_servers.country, vpn_servers.country_code").
		Order("connection_count DESC").
		Limit(limit).
		Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("querying top servers: %w", err)
	}
	return out, nil
}

// FindUserByIDAdmin looks up any user by UUID for admin use.
// Wraps the sentinel error so callers can use errors.Is(err, ErrNotFound).
func FindUserByIDAdmin(ctx context.Context, db *gorm.DB, id string) (*model.User, error) {
	if db == nil {
		return nil, errNilDB
	}
	var user model.User
	result := db.WithContext(ctx).First(&user, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("finding user %s: %w", id, result.Error)
	}
	return &user, nil
}
