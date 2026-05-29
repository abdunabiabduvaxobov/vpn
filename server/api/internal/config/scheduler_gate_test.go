package config_test

import (
	"testing"

	"vpnapp/server/api/internal/config"
)

// Wave 0 RED-first test for the RUN_SCHEDULER gate helper (PERF-06 / D-09b).
// References config.ShouldRunScheduler — not yet built (plan 06) — so this file
// fails to compile until the helper lands. This is the LOAD-BEARING PERF-06 (d)
// assertion: a RUN_SCHEDULER=false replica must fire no scheduler jobs, and the
// pure gate decision is what main.go branches on before calling scheduler.Start.
//
// Rule (RESEARCH §D-09b: default true for single-replica v2.2.0):
//   default ON — any value other than an explicit falsey value enables the
//   scheduler. Falsey set = {"false", "0", "no"} (case-insensitive). Empty
//   string ("" — env var unset) defaults to true so the single-replica deploy
//   keeps running periodic work with no config change.
func TestShouldRunScheduler(t *testing.T) {
	cases := []struct {
		name     string
		envValue string
		want     bool
	}{
		{"unset defaults true", "", true},
		{"explicit true", "true", true},
		{"uppercase TRUE", "TRUE", true},
		{"numeric 1", "1", true},
		{"yes", "yes", true},
		{"arbitrary value is truthy (default ON)", "primary", true},
		{"explicit false", "false", false},
		{"uppercase FALSE", "FALSE", false},
		{"numeric 0", "0", false},
		{"no", "no", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := config.ShouldRunScheduler(tc.envValue)
			if got != tc.want {
				t.Errorf("ShouldRunScheduler(%q) = %v, want %v", tc.envValue, got, tc.want)
			}
		})
	}
}
