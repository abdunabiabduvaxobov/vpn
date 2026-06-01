package internal

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config holds all tunnel server configuration.
type Config struct {
	// Port to listen on (typically 443 to mimic HTTPS)
	Port int `json:"port"`

	// Protocol: "vless-reality" or "amneziawg"
	Protocol string `json:"protocol"`

	// Clients is the list of UUIDs (VLESS user IDs) allowed to connect.
	// Each entry becomes a VLESS client in the xray-core inbound config.
	// Required for all xray-based protocols (vless-reality, vless-ws).
	// Not used when Protocol is "amneziawg".
	Clients []string `json:"clients"`

	// REALITY configuration (used when Protocol is "vless-reality")
	Reality RealityConfig `json:"reality"`

	// WebSocket configuration for CDN transport.
	// When WebSocket.Enabled is true, xray-core opens an additional VLESS inbound
	// on WebSocket transport so Nginx can proxy CDN (Cloudflare) traffic to it.
	WebSocket WebSocketServerConfig `json:"websocket"`

	// AWG is the AmneziaWG server configuration.
	// When AWG.Enabled is true the AWG server starts alongside xray-core on a
	// separate UDP port (default 51820), allowing clients to choose either protocol.
	AWG AWGServerConfig `json:"awg"`

	// Health check endpoint port (separate from tunnel port)
	HealthPort int `json:"health_port"`

	// --- Optional API heartbeat (ADMIN-07) ---
	// When all of APIBaseURL, ServerID and HeartbeatSecret are set, the tunnel
	// starts a background emitter that POSTs to the API's
	// /api/v1/internal/servers/:id/heartbeat so /readyz sees a fresh
	// last_seen_at. These are OPTIONAL and NOT validated — an AWG-only or dev
	// node without this config runs exactly as before (no emitter started).
	APIBaseURL               string `json:"api_base_url"`
	ServerID                 string `json:"server_id"`
	HeartbeatSecret          string `json:"heartbeat_secret"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
}

// WebSocketServerConfig holds settings for the optional WebSocket CDN inbound.
// This inbound listens on localhost only — Nginx proxies from the public CDN
// domain down to this local port.
type WebSocketServerConfig struct {
	// Enabled controls whether the WebSocket inbound is started.
	Enabled bool `json:"enabled"`

	// Port is the local port xray-core listens on for WebSocket connections.
	// Nginx proxies the public HTTPS/WebSocket traffic to this port.
	// Must not be exposed publicly — bind is 127.0.0.1 only.
	Port int `json:"port"`

	// Path is the WebSocket upgrade path to accept, e.g. "/ws".
	// Must match the path configured in Nginx and on the client.
	Path string `json:"path"`
}

// RealityConfig holds VLESS+REALITY specific settings.
// REALITY works by impersonating a real TLS server during the handshake.
// Only clients with the correct private key can complete the connection.
type RealityConfig struct {
	// X25519 private key for REALITY handshake (base64)
	PrivateKey string `json:"private_key"`

	// Corresponding public key (shared with clients)
	PublicKey string `json:"public_key"`

	// Short IDs for client authentication (hex strings)
	ShortIDs []string `json:"short_ids"`

	// Destination: the real website to impersonate (e.g., "www.microsoft.com:443")
	// REALITY will proxy the TLS handshake to this server for unauthenticated clients
	Dest string `json:"dest"`

	// Server names (SNI values) to accept in TLS ClientHello
	ServerNames []string `json:"server_names"`
}

// LoadConfig reads and parses the configuration file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

// validate checks that the configuration has all required fields.
func (c *Config) validate() error {
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", c.Port)
	}

	switch c.Protocol {
	case "vless-reality":
		if len(c.Clients) == 0 {
			return fmt.Errorf("clients must have at least one UUID for protocol %q", c.Protocol)
		}
		if c.Reality.PrivateKey == "" {
			return fmt.Errorf("reality.private_key is required")
		}
		if c.Reality.Dest == "" {
			return fmt.Errorf("reality.dest is required")
		}
		if len(c.Reality.ServerNames) == 0 {
			return fmt.Errorf("reality.server_names must have at least one entry")
		}
		if len(c.Reality.ShortIDs) == 0 {
			return fmt.Errorf("reality.short_ids must have at least one entry")
		}
	case "amneziawg":
		// A server running as "amneziawg" primary does not need xray-core config.
		// The AWG section is validated below.
	default:
		return fmt.Errorf("unsupported protocol: %s (supported: vless-reality, amneziawg)", c.Protocol)
	}

	if c.WebSocket.Enabled {
		if c.WebSocket.Port <= 0 || c.WebSocket.Port > 65535 {
			return fmt.Errorf("websocket.port must be between 1 and 65535, got %d", c.WebSocket.Port)
		}
		if c.WebSocket.Port == c.Port {
			return fmt.Errorf("websocket.port must differ from port (%d)", c.Port)
		}
		if c.WebSocket.Path == "" {
			c.WebSocket.Path = "/ws"
		}
	}

	// AWG can run alongside any primary protocol.
	if c.AWG.Enabled {
		if err := c.AWG.validate(); err != nil {
			return fmt.Errorf("awg: %w", err)
		}
	}

	if c.HealthPort <= 0 {
		c.HealthPort = 8080
	}

	return nil
}
