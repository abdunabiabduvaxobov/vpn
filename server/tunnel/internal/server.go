package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	// XRay-core — import only what VLESS+REALITY/WebSocket needs
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/blackhole"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/transport/internet/reality"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	_ "github.com/xtls/xray-core/transport/internet/websocket"

	"go.uber.org/zap"
)

// TunnelServer manages the VPN tunnel lifecycle.
// It wraps XRay-core for VLESS+REALITY and exposes a health check endpoint.
// When websocket is enabled in config, a second xray-core instance is started
// for the WebSocket CDN inbound (listening on localhost only, proxied by Nginx).
type TunnelServer struct {
	config          *Config
	logger          *zap.Logger
	xrayInstance    *core.Instance
	wsXrayInstance  *core.Instance
	healthServer    *http.Server
	mu              sync.Mutex
	running         bool
}

// NewTunnelServer creates a new tunnel server instance.
func NewTunnelServer(config *Config, logger *zap.Logger) (*TunnelServer, error) {
	return &TunnelServer{
		config: config,
		logger: logger,
	}, nil
}

// Start initializes and starts the VLESS+REALITY tunnel via XRay-core.
//
// This creates an XRay-core instance that:
// 1. Listens on port 443 (looks like a normal HTTPS server)
// 2. Uses REALITY to impersonate a legitimate TLS server
// 3. Accepts only clients with the correct REALITY key
// 4. Forwards authenticated client traffic to the internet
func (s *TunnelServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server is already running")
	}

	// Build XRay-core JSON configuration
	xrayConfig := s.buildXRayConfig()

	s.logger.Info("starting xray-core",
		zap.String("protocol", s.config.Protocol),
		zap.String("dest", s.config.Reality.Dest),
		zap.Strings("server_names", s.config.Reality.ServerNames),
		zap.Int("port", s.config.Port),
	)

	// Serialize config to JSON
	jsonConfig, err := json.Marshal(xrayConfig)
	if err != nil {
		return fmt.Errorf("marshaling xray config: %w", err)
	}

	// Parse JSON config into xray-core protobuf config
	xrayPbConfig, err := serial.LoadJSONConfig(bytes.NewReader(jsonConfig))
	if err != nil {
		return fmt.Errorf("loading xray config: %w", err)
	}

	// Create XRay-core instance from protobuf config
	instance, err := core.New(xrayPbConfig)
	if err != nil {
		return fmt.Errorf("creating xray instance: %w", err)
	}

	// Start the tunnel
	if err := instance.Start(); err != nil {
		return fmt.Errorf("starting xray instance: %w", err)
	}

	s.xrayInstance = instance
	s.logger.Info("xray-core started — VLESS+REALITY tunnel active",
		zap.Int("port", s.config.Port),
	)

	// Optionally start the WebSocket CDN inbound on a separate xray-core instance.
	// This instance listens on localhost only; Nginx proxies Cloudflare traffic to it.
	if s.config.WebSocket.Enabled {
		if err := s.startWebSocketInbound(); err != nil {
			// WebSocket is optional — log the error but do not abort the REALITY tunnel.
			s.logger.Error("failed to start websocket inbound — CDN transport unavailable",
				zap.Error(err),
				zap.Int("ws_port", s.config.WebSocket.Port),
			)
		}
	}

	// Start health check server
	s.startHealthServer()

	s.running = true
	return nil
}

// startWebSocketInbound creates and starts a second xray-core instance for the
// WebSocket CDN inbound. It listens on 127.0.0.1:ws_port so that Nginx can
// proxy Cloudflare WebSocket connections to it.
//
// This runs as a separate xray-core instance (not merged into the REALITY one)
// so that each transport has its own clearly scoped configuration and the
// WebSocket inbound can be toggled independently without touching REALITY.
func (s *TunnelServer) startWebSocketInbound() error {
	wsConfig := s.buildWebSocketConfig()

	jsonConfig, err := json.Marshal(wsConfig)
	if err != nil {
		return fmt.Errorf("marshaling websocket xray config: %w", err)
	}

	xrayPbConfig, err := serial.LoadJSONConfig(bytes.NewReader(jsonConfig))
	if err != nil {
		return fmt.Errorf("loading websocket xray config: %w", err)
	}

	wsInstance, err := core.New(xrayPbConfig)
	if err != nil {
		return fmt.Errorf("creating websocket xray instance: %w", err)
	}

	if err := wsInstance.Start(); err != nil {
		return fmt.Errorf("starting websocket xray instance: %w", err)
	}

	s.wsXrayInstance = wsInstance
	s.logger.Info("websocket CDN inbound active",
		zap.Int("port", s.config.WebSocket.Port),
		zap.String("path", s.config.WebSocket.Path),
	)
	return nil
}

// Stop gracefully shuts down the tunnel server.
func (s *TunnelServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	if s.xrayInstance != nil {
		if err := s.xrayInstance.Close(); err != nil {
			s.logger.Error("error closing xray instance", zap.Error(err))
		}
		s.xrayInstance = nil
	}

	if s.wsXrayInstance != nil {
		if err := s.wsXrayInstance.Close(); err != nil {
			s.logger.Error("error closing websocket xray instance", zap.Error(err))
		}
		s.wsXrayInstance = nil
	}

	if s.healthServer != nil {
		s.healthServer.Close()
	}

	s.running = false
	s.logger.Info("tunnel server stopped")
	return nil
}

// ReloadClients atomically swaps the tunnel's active VLESS UUID set and restarts
// the running xray-core instance(s) so that, after the call returns, ONLY the
// passed UUIDs are admitted at the wire (HARD-02 wire enforcement, D-05).
//
// CONNECTION-DROP TRADEOFF (08-RESEARCH §2.2): xray-core's core.Instance has NO
// in-place hot reload — the only way to change the admitted client set is to
// Close the old instance and start a fresh one from a rebuilt config. That Close
// DROPS every live connection on the affected instance. This is accepted
// pre-launch ("free hand to break things", no paying users) and is DEBOUNCED
// upstream (the tunnel's heartbeat-driven pull loop, Task 3b) so a burst of
// rotations coalesces into a single reload — giving a documented 30-60s wire
// propagation floor rather than a reload per rotation.
//
// Concurrency: the whole swap runs under s.mu so a Start/Stop/ReloadClients can
// never interleave and leave a half-built instance visible.
//
// Failure handling: on a core.New / Start error the method returns the error
// WITHOUT clearing s.xrayInstance to nil for a stale handle — it leaves the
// server flagged not-running and surfaces a clear failure so the supervising
// process (or the next pull tick) can react, rather than silently swallowing a
// reload that left no instance serving.
func (s *TunnelServer) ReloadClients(uuids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Swap the active set; buildXRayConfig reads s.config.Clients.
	s.config.Clients = uuids

	// --- REALITY (primary) instance ---
	xrayConfig := s.buildXRayConfig()
	jsonConfig, err := json.Marshal(xrayConfig)
	if err != nil {
		return fmt.Errorf("reload: marshaling xray config: %w", err)
	}
	xrayPbConfig, err := serial.LoadJSONConfig(bytes.NewReader(jsonConfig))
	if err != nil {
		return fmt.Errorf("reload: loading xray config: %w", err)
	}
	instance, err := core.New(xrayPbConfig)
	if err != nil {
		return fmt.Errorf("reload: creating xray instance: %w", err)
	}
	if err := instance.Start(); err != nil {
		// Do not start; close the just-created instance to avoid leaking it.
		_ = instance.Close()
		return fmt.Errorf("reload: starting xray instance: %w", err)
	}
	// New instance is live — now close the old one (drops its connections).
	if s.xrayInstance != nil {
		if cerr := s.xrayInstance.Close(); cerr != nil {
			s.logger.Error("reload: error closing previous xray instance", zap.Error(cerr))
		}
	}
	s.xrayInstance = instance

	// --- WebSocket instance (only if enabled) ---
	if s.config.WebSocket.Enabled {
		wsConfig := s.buildWebSocketConfig()
		wsJSON, werr := json.Marshal(wsConfig)
		if werr != nil {
			return fmt.Errorf("reload: marshaling websocket config: %w", werr)
		}
		wsPb, werr := serial.LoadJSONConfig(bytes.NewReader(wsJSON))
		if werr != nil {
			return fmt.Errorf("reload: loading websocket config: %w", werr)
		}
		wsInstance, werr := core.New(wsPb)
		if werr != nil {
			return fmt.Errorf("reload: creating websocket instance: %w", werr)
		}
		if werr := wsInstance.Start(); werr != nil {
			_ = wsInstance.Close()
			return fmt.Errorf("reload: starting websocket instance: %w", werr)
		}
		if s.wsXrayInstance != nil {
			if cerr := s.wsXrayInstance.Close(); cerr != nil {
				s.logger.Error("reload: error closing previous websocket instance", zap.Error(cerr))
			}
		}
		s.wsXrayInstance = wsInstance
	}

	s.logger.Info("xray client set reloaded — live connections dropped (xray-core has no hot reload)",
		zap.Int("client_count", len(uuids)),
	)
	return nil
}

// buildXRayConfig creates the XRay-core JSON configuration for VLESS+REALITY.
// This configuration makes the server appear as a legitimate HTTPS server
// to any network observer or DPI system.
func (s *TunnelServer) buildXRayConfig() map[string]interface{} {
	// Build the clients list from the configured UUIDs.
	clients := make([]map[string]interface{}, 0, len(s.config.Clients))
	for _, uid := range s.config.Clients {
		clients = append(clients, map[string]interface{}{
			"id":   uid,
			"flow": "xtls-rprx-vision",
		})
	}

	return map[string]interface{}{
		"log": map[string]interface{}{
			"loglevel": "warning",
		},
		"inbounds": []map[string]interface{}{
			{
				"port":     s.config.Port,
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients":    clients,
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":  "tcp",
					"security": "reality",
					"realitySettings": map[string]interface{}{
						"show":        false,
						"dest":        s.config.Reality.Dest,
						"xver":        0,
						"serverNames": s.config.Reality.ServerNames,
						"privateKey":  s.config.Reality.PrivateKey,
						"shortIds":    s.config.Reality.ShortIDs,
					},
				},
			},
		},
		"outbounds": []map[string]interface{}{
			{
				"protocol": "freedom",
				"tag":      "direct",
			},
			{
				"protocol": "blackhole",
				"tag":      "block",
			},
		},
	}
}

// buildWebSocketConfig creates the XRay-core JSON configuration for VLESS over WebSocket CDN.
//
// This configuration makes the server accept VLESS traffic through a WebSocket
// connection. Nginx sits in front and proxies the Cloudflare CDN WebSocket
// connection to this local port. Key differences from the REALITY inbound:
//   - network is "ws" (WebSocket), not "tcp"
//   - No realitySettings — Cloudflare terminates TLS before us
//   - Listens on 127.0.0.1 only — never exposed directly to the internet
//   - flow is empty for clients — xtls-rprx-vision is TCP-only
func (s *TunnelServer) buildWebSocketConfig() map[string]interface{} {
	// Build the clients list from the configured UUIDs.
	// flow must be empty for WebSocket transport — xtls-rprx-vision is TCP-only.
	wsClients := make([]map[string]interface{}, 0, len(s.config.Clients))
	for _, uid := range s.config.Clients {
		wsClients = append(wsClients, map[string]interface{}{
			"id":   uid,
			"flow": "",
		})
	}

	return map[string]interface{}{
		"log": map[string]interface{}{
			"loglevel": "warning",
		},
		"inbounds": []map[string]interface{}{
			{
				// Bind on localhost only — Nginx proxies to this port.
				// Never expose this port to the public internet directly.
				"listen":   "127.0.0.1",
				"port":     s.config.WebSocket.Port,
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients":    wsClients,
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network": "ws",
					"wsSettings": map[string]interface{}{
						"path": s.config.WebSocket.Path,
					},
				},
				"tag": "ws-in",
			},
		},
		"outbounds": []map[string]interface{}{
			{
				"protocol": "freedom",
				"tag":      "direct",
			},
			{
				"protocol": "blackhole",
				"tag":      "block",
			},
		},
	}
}

// startHealthServer runs an HTTP server for health checks and metrics.
func (s *TunnelServer) startHealthServer() {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "healthy",
			"protocol": s.config.Protocol,
			"running":  s.running,
			"xray":     s.xrayInstance != nil,
		})
	})

	// Bind on localhost only — the health endpoint must not be reachable
	// from the public internet. Docker/Compose health checks use localhost.
	s.healthServer = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", s.config.HealthPort),
		Handler: mux,
	}

	go func() {
		if err := s.healthServer.ListenAndServe(); err != http.ErrServerClosed {
			s.logger.Error("health server error", zap.Error(err))
		}
	}()

	s.logger.Info("health server started", zap.Int("port", s.config.HealthPort))
}
