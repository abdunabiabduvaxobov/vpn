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
