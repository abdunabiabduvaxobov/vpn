package handler

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// wantBcryptCost is the HARD-11 (D-19) target work factor for newly-created
// admin password hashes. 12 (~4x the work of 10) keeps offline cracking of a
// leaked admin hash expensive without an unacceptable login-latency hit.
const wantBcryptCost = 12

// productionAdminHashCost reproduces the cost the production admin-password
// hashing paths use today. Both prod sites — createadmin/main.go:77 and
// auth.go:201 (AdminChangePassword) — call
// bcrypt.GenerateFromPassword(..., bcrypt.DefaultCost). DefaultCost == 10.
//
// HARD-11 replaces bcrypt.DefaultCost with a cost-12 constant at both sites.
// When that lands, change this helper to call the shared production hashing
// function/constant so the assertion below tracks the real code path rather
// than this mirror.
func productionAdminHashCost(t *testing.T) int {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}
	cost, err := bcrypt.Cost(hash)
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	return cost
}

// TestAdminPasswordHash_IsCost12 pins HARD-11.
//
// RED now: the production paths use bcrypt.DefaultCost (10), so this asserts
// 10 == 12 and fails. Flips GREEN when HARD-11 bumps both hashing sites to
// cost 12.
func TestAdminPasswordHash_IsCost12(t *testing.T) {
	got := productionAdminHashCost(t)
	if got != wantBcryptCost {
		t.Errorf("HARD-11: admin password hash uses bcrypt cost %d, want %d", got, wantBcryptCost)
	}
}
