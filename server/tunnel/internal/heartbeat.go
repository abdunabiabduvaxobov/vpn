package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// heartbeatBody is the JSON the tunnel POSTs on each tick. concurrent_conns is
// sent for forward-compat; the API does not yet consume it (v1). load_percent is
// best-effort — 0 is acceptable when the tunnel cannot cheaply measure load.
type heartbeatBody struct {
	LoadPercent     int `json:"load_percent"`
	ConcurrentConns int `json:"concurrent_conns"`
}

// StartHeartbeat runs a best-effort liveness emitter: on each interval tick it
// POSTs to {apiBaseURL}/api/v1/internal/servers/{serverID}/heartbeat with the
// X-Internal-Secret header, so the API's /readyz tunnel check sees a fresh
// last_seen_at (ADMIN-07). It is the ONLY tunnel-side change in Phase 7.
//
// Failure policy: a failed POST is logged at Warn and the loop continues — a
// transient API outage or network blip MUST NOT crash the tunnel (the tunnel
// keeps serving VPN traffic regardless of heartbeat delivery). Each request has
// a 5s client timeout so a hung API cannot stall the ticker indefinitely.
//
// The function returns when ctx is cancelled (graceful shutdown). The caller is
// expected to run it in a goroutine and cancel ctx in the shutdown path.
func StartHeartbeat(ctx context.Context, apiBaseURL, serverID, secret string, interval time.Duration, logger *zap.Logger) {
	url := apiBaseURL + "/api/v1/internal/servers/" + serverID + "/heartbeat"
	client := &http.Client{Timeout: 5 * time.Second}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("tunnel heartbeat emitter started",
		zap.String("url", url),
		zap.Duration("interval", interval),
	)

	// Emit one immediately so last_seen_at is populated without waiting a full
	// interval after (re)start.
	postOnce(ctx, client, url, secret, logger)

	for {
		select {
		case <-ctx.Done():
			logger.Info("tunnel heartbeat emitter stopping")
			return
		case <-ticker.C:
			postOnce(ctx, client, url, secret, logger)
		}
	}
}

// clientReloader is the narrow capability StartClientSync needs from the tunnel
// server: swap the active VLESS UUID set and regen+reload. *TunnelServer
// satisfies it via ReloadClients. Kept as an interface so the sync loop is
// decoupled from the concrete server and unit-testable without xray-core.
type clientReloader interface {
	ReloadClients(uuids []string) error
}

// vlessClientsResponse is the active-set payload the API returns at
// GET /api/v1/internal/servers/:id/vless-clients (HARD-02). The etag is a stable
// hash of the active UUID set so the tunnel can cheaply detect a change.
type vlessClientsResponse struct {
	UUIDs []string `json:"uuids"`
	ETag  string   `json:"etag"`
}

// StartClientSync is the HARD-02 wire-enforcement pull loop. On each interval
// tick it GETs the active per-user VLESS UUID set from the API
// (/api/v1/internal/servers/:id/vless-clients, same X-Internal-Secret gate as
// the heartbeat), and when the returned ETag differs from the last applied set
// it DEBOUNCES (coalescing a burst of rotations) and then calls
// server.ReloadClients with the new set.
//
// PROPAGATION FLOOR (Open-Risk 2 — DO NOT HIDE): the API rotates a user's UUID
// IMMEDIATELY (the new identity is active and the old is_active=false in the same
// tx), but the WIRE lags one reload cycle = the pull interval (>=30s) + the
// debounce window (debounce). That is a realistic 30-60s wire-enforcement floor.
// SC#2 "rotates" is therefore satisfied at the API instantly and at the wire
// within 30-60s — not sub-second, because xray-core has no hot reload (each
// reload is connection-dropping, so we MUST batch).
//
// Reliability: a failed pull is logged at Warn and the loop continues (a
// transient API blip MUST NOT stop wire sync or crash the tunnel). An UNCHANGED
// set (ETag match) is skipped entirely — no reload, so no needless connection
// drops. Returns when ctx is cancelled (graceful shutdown).
func StartClientSync(ctx context.Context, server clientReloader, apiBaseURL, serverID, secret string, interval, debounce time.Duration, logger *zap.Logger) {
	url := apiBaseURL + "/api/v1/internal/servers/" + serverID + "/vless-clients"
	client := &http.Client{Timeout: 5 * time.Second}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("tunnel vless-client sync started",
		zap.String("url", url),
		zap.Duration("interval", interval),
		zap.Duration("debounce", debounce),
		zap.String("propagation_floor", "30-60s wire enforcement (pull interval + debounce; xray-core has no hot reload)"),
	)

	var lastETag string
	for {
		select {
		case <-ctx.Done():
			logger.Info("tunnel vless-client sync stopping")
			return
		case <-ticker.C:
			resp, etag, err := fetchActiveClients(ctx, client, url, secret)
			if err != nil {
				logger.Warn("vless-sync: fetch failed", zap.Error(err))
				continue
			}
			if etag == lastETag {
				// Unchanged active set — skip the connection-dropping reload.
				continue
			}
			// WR-02: refuse to reload to an EMPTY active set. A transient empty
			// read (migration window, bad mass-revoke, or a query the handler
			// turns into uuids=[]) would otherwise rebuild xray with zero clients
			// and Close the live instance — a full-server outage dropping every
			// connection and admitting nobody. Treat empty as suspicious: keep the
			// previous client set and retry on the next tick WITHOUT advancing
			// lastETag, so a genuine (durable) empty set still converges once a
			// non-empty read or an explicit signal arrives. xray-core has no hot
			// reload, so the cost of a wrong empty reload is total, not partial.
			if len(resp.UUIDs) == 0 {
				logger.Warn("vless-sync: refusing to reload to an EMPTY active set — keeping previous client set",
					zap.String("etag", etag),
				)
				// Do NOT advance lastETag: retry on the next tick.
				continue
			}
			// DEBOUNCE: wait to coalesce a burst of rotations into one reload.
			// Honour ctx so shutdown is not delayed by the debounce window.
			if debounce > 0 {
				select {
				case <-ctx.Done():
					logger.Info("tunnel vless-client sync stopping (during debounce)")
					return
				case <-time.After(debounce):
				}
			}
			if err := server.ReloadClients(resp.UUIDs); err != nil {
				logger.Error("vless-sync: ReloadClients failed — keeping previous active set",
					zap.Error(err))
				// Do NOT advance lastETag: retry on the next tick.
				continue
			}
			lastETag = etag
			logger.Info("vless-sync: active client set reloaded",
				zap.Int("client_count", len(resp.UUIDs)),
				zap.String("etag", etag),
			)
		}
	}
}

// fetchActiveClients performs a single authed GET of the active UUID set and
// returns the decoded body plus the effective ETag (the response's etag field,
// falling back to the ETag header). Any transport / decode error is surfaced to
// the caller, which logs-and-continues.
func fetchActiveClients(ctx context.Context, client *http.Client, url, secret string) (vlessClientsResponse, string, error) {
	var out vlessClientsResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return out, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Internal-Secret", secret)

	resp, err := client.Do(req)
	if err != nil {
		return out, "", fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return out, "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, "", fmt.Errorf("decode: %w", err)
	}
	etag := out.ETag
	if etag == "" {
		etag = resp.Header.Get("ETag")
	}
	return out, etag, nil
}

// postOnce sends a single heartbeat. Best-effort: any error is logged at Warn
// and swallowed so the emitter loop never crashes the tunnel.
func postOnce(ctx context.Context, client *http.Client, url, secret string, logger *zap.Logger) {
	payload, err := json.Marshal(heartbeatBody{LoadPercent: 0, ConcurrentConns: 0})
	if err != nil {
		logger.Warn("heartbeat: marshal failed", zap.Error(err))
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		logger.Warn("heartbeat: build request failed", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", secret)

	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("heartbeat: post failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		logger.Warn("heartbeat: unexpected status", zap.String("status", fmt.Sprintf("%d", resp.StatusCode)))
	}
}
