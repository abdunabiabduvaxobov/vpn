package handler_test

import (
	"testing"

	"vpnapp/server/api/internal/config"

	"golang.org/x/crypto/bcrypt"
)

// TestBcryptCostIs12 verifies HARD-11 / S4-5: the production password-hash cost
// constant is 12 and a hash generated with it reports cost 12. Both the
// createadmin bootstrap and the admin password-change path use
// config.BcryptCost, so pinning the constant + the resulting hash cost guards
// the whole production path without re-running the slow CLI/handler here.
func TestBcryptCostIs12(t *testing.T) {
	if config.BcryptCost != 12 {
		t.Fatalf("config.BcryptCost = %d, want 12", config.BcryptCost)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), config.BcryptCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	cost, err := bcrypt.Cost(hash)
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if cost != 12 {
		t.Fatalf("hash cost = %d, want 12", cost)
	}

	// Higher than the library default (10) — the whole point of HARD-11.
	if config.BcryptCost <= bcrypt.DefaultCost {
		t.Fatalf("BcryptCost (%d) must exceed bcrypt.DefaultCost (%d)", config.BcryptCost, bcrypt.DefaultCost)
	}
}
