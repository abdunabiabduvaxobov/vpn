package main

import (
	"testing"
	"time"
)

// TestServerConfig locks the PERF-09 / D-09c Fiber hardening values. These
// four timeout/limit fields defend against slowloris (ReadTimeout/IdleTimeout),
// oversized-body DoS (BodyLimit), and slow-consumer write stalls (WriteTimeout)
// — see the threat register (T-06-SLOWLORIS, T-06-BODYDOS). Prefork MUST stay
// false because prefork forks separate processes, each with its own DB pool,
// which would defeat the shared tuned pool in repository.NewDB.
func TestServerConfig(t *testing.T) {
	cfg := buildFiberConfig(nil)

	if got, want := cfg.BodyLimit, 64*1024; got != want {
		t.Errorf("BodyLimit = %d, want %d (64KB — PERF-09 body-DoS cap)", got, want)
	}
	if got, want := cfg.ReadTimeout, 15*time.Second; got != want {
		t.Errorf("ReadTimeout = %v, want %v (PERF-09 slowloris defense)", got, want)
	}
	if got, want := cfg.WriteTimeout, 30*time.Second; got != want {
		t.Errorf("WriteTimeout = %v, want %v (PERF-09)", got, want)
	}
	if got, want := cfg.IdleTimeout, 120*time.Second; got != want {
		t.Errorf("IdleTimeout = %v, want %v (audit §6.2 idle-keepalive close)", got, want)
	}
	if cfg.Prefork {
		t.Error("Prefork = true, want false — prefork breaks the shared DB pool")
	}

	// Guard the existing fields the hardening must not regress.
	if cfg.AppName != "VPN API Server" {
		t.Errorf("AppName = %q, want %q", cfg.AppName, "VPN API Server")
	}
	if !cfg.EnableTrustedProxyCheck {
		t.Error("EnableTrustedProxyCheck = false, want true")
	}
	if cfg.TrustedProxies == nil || len(cfg.TrustedProxies) != 0 {
		t.Errorf("TrustedProxies = %v, want empty non-nil slice (trust nothing)", cfg.TrustedProxies)
	}
}
