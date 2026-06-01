package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vpnapp/server/tunnel/internal"

	"go.uber.org/zap"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "config.json", "Path to server configuration file")
	flag.Parse()

	// Initialize structured logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Load server configuration
	config, err := internal.LoadConfig(*configPath)
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	// --- Start xray-core tunnel (VLESS+REALITY / WebSocket) ---
	// Only started when the primary protocol is not "amneziawg".  When running
	// as an AWG-only node, the xray-core binary is not needed.
	var xrayServer *internal.TunnelServer
	if config.Protocol != "amneziawg" {
		xrayServer, err = internal.NewTunnelServer(config, logger)
		if err != nil {
			logger.Fatal("failed to create tunnel server", zap.Error(err))
		}

		if err := xrayServer.Start(); err != nil {
			logger.Fatal("failed to start tunnel server", zap.Error(err))
		}

		logger.Info("xray tunnel server started",
			zap.String("listen", fmt.Sprintf(":%d", config.Port)),
			zap.String("protocol", config.Protocol),
		)
	}

	// --- Start AmneziaWG server (optional, runs alongside xray-core) ---
	// Both protocols can be active simultaneously: xray on TCP/443 and AWG on UDP/51820.
	var awgServer *internal.AWGServer
	if config.AWG.Enabled {
		awgServer = internal.NewAWGServer(&config.AWG, logger)
		if err := awgServer.Start(); err != nil {
			// AWG failure is non-fatal when xray is running — log and continue.
			logger.Error("failed to start amneziawg server", zap.Error(err))
		} else {
			logger.Info("amneziawg server started",
				zap.String("listen", fmt.Sprintf("udp/:%d", config.AWG.ListenPort)),
			)
		}
	}

	// --- Start API heartbeat emitter (ADMIN-07, optional) ---
	// Only starts when all three required fields are configured, so AWG-only and
	// dev nodes without the heartbeat config run unchanged. A minimum 30s interval
	// is enforced to avoid hammering the API.
	var hbCancel context.CancelFunc
	if config.APIBaseURL != "" && config.ServerID != "" && config.HeartbeatSecret != "" {
		interval := 30 * time.Second
		if config.HeartbeatIntervalSeconds > 0 {
			if cfgd := time.Duration(config.HeartbeatIntervalSeconds) * time.Second; cfgd > interval {
				interval = cfgd
			}
		}
		var hbCtx context.Context
		hbCtx, hbCancel = context.WithCancel(context.Background())
		go internal.StartHeartbeat(hbCtx, config.APIBaseURL, config.ServerID, config.HeartbeatSecret, interval, logger)
	}

	// --- Wait for shutdown signal (SIGINT or SIGTERM) ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	logger.Info("received shutdown signal", zap.String("signal", sig.String()))

	// --- Graceful shutdown ---

	if hbCancel != nil {
		hbCancel()
	}

	if awgServer != nil {
		if err := awgServer.Stop(); err != nil {
			logger.Error("error stopping amneziawg server", zap.Error(err))
		}
	}

	if xrayServer != nil {
		if err := xrayServer.Stop(); err != nil {
			logger.Error("error stopping xray tunnel server", zap.Error(err))
		}
	}

	logger.Info("tunnel server stopped")
}
