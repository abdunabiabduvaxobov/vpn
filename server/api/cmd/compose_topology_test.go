package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestComposeDataTierSplit locks PERF-03 / D-01: the data tier (postgres + redis)
// lives in docker-compose.data.yml, the app tier (docker-compose.prod.yml) declares
// NEITHER stateful service, and the api's DATABASE_URL/REDIS_URL are host-parameterized
// via ${DB_HOST:-postgres} / ${REDIS_HOST:-redis} so the physical off-host move is a
// zero-code .env change. Static and Docker-free: it parses both compose YAMLs from the
// repo root (structural parse, so the many "postgres"/"redis" mentions in the file
// comments cannot false-match the way a grep would).
func TestComposeDataTierSplit(t *testing.T) {
	root := repoRoot(t)

	type service struct {
		Environment map[string]any `yaml:"environment"`
	}
	type composeFile struct {
		Services map[string]service `yaml:"services"`
	}

	parse := func(name string) composeFile {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var cf composeFile
		if err := yaml.Unmarshal(raw, &cf); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		return cf
	}

	data := parse("docker-compose.data.yml")
	prod := parse("docker-compose.prod.yml")

	// (1) The data tier owns BOTH stateful services.
	for _, svc := range []string{"postgres", "redis"} {
		if _, ok := data.Services[svc]; !ok {
			t.Errorf("docker-compose.data.yml is missing the %q service (PERF-03 data tier)", svc)
		}
	}

	// (2) The app tier declares NEITHER stateful service (they moved to the data tier).
	for _, svc := range []string{"postgres", "redis"} {
		if _, ok := prod.Services[svc]; ok {
			t.Errorf("docker-compose.prod.yml must not declare %q — it lives in docker-compose.data.yml now (PERF-03)", svc)
		}
	}
	if _, ok := prod.Services["api"]; !ok {
		t.Error("docker-compose.prod.yml should still declare the api service")
	}

	// (3) Host-parameterized connection strings: the off-host move is a zero-code .env change.
	api, ok := prod.Services["api"]
	if !ok {
		t.Fatal("docker-compose.prod.yml has no api service to check DB_HOST/REDIS_HOST")
	}
	dbURL := fmt.Sprintf("%v", api.Environment["DATABASE_URL"])
	if !strings.Contains(dbURL, "${DB_HOST:-postgres}") {
		t.Errorf("api DATABASE_URL must be host-parameterized via ${DB_HOST:-postgres}; got %q", dbURL)
	}
	redisURL := fmt.Sprintf("%v", api.Environment["REDIS_URL"])
	if !strings.Contains(redisURL, "${REDIS_HOST:-redis}") {
		t.Errorf("api REDIS_URL must be host-parameterized via ${REDIS_HOST:-redis}; got %q", redisURL)
	}
}

// repoRoot walks up from this test file's directory until it finds
// docker-compose.data.yml, so the test is independent of the working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "docker-compose.data.yml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate repo root (docker-compose.data.yml not found walking up from the test file)")
	return ""
}
