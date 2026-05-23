// Package repository — plan_repo_stub.go
//
// WORKTREE-ONLY STUB. Plan 03-04 (this worktree) depends on functions declared
// in plan 03-03's plan_repo.go, which lands in a sibling Wave-2 worktree. The
// orchestrator's post-Wave-2 merge will REPLACE these stubs with 03-03's full
// implementation (FindPlanByID, FindPlanByCode, FindSystemPlanID,
// ListServersForPlan, IsServerAllowedForPlan, SetUserPlan, plus the rest of
// the plan/offer/plan_server CRUD).
//
// **DELETE THIS FILE during the merge.** Both worktrees declare these
// functions; when 03-03's plan_repo.go arrives, this stub MUST be deleted to
// avoid duplicate-symbol compile errors. The SUMMARY.md for 03-04 documents
// the merge step explicitly.
//
// These stubs are functionally correct sqlite-compatible implementations so
// in-worktree tests pass; they are simply missing 03-03's additional CRUD
// helpers (ListActivePlans, CreatePlan, etc.) that 03-04 does not need.
package repository

import (
	"errors"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

func FindPlanByID(db *gorm.DB, planID string) (*model.Plan, error) {
	var plan model.Plan
	if err := db.Where("id = ?", planID).First(&plan).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &plan, nil
}

func FindPlanByCode(db *gorm.DB, code string) (*model.Plan, error) {
	var plan model.Plan
	if err := db.Where("code = ?", code).First(&plan).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &plan, nil
}

func FindSystemPlanID(db *gorm.DB) (string, error) {
	var plan model.Plan
	if err := db.Where("is_system = ?", true).First(&plan).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	return plan.ID, nil
}

func ListServersForPlan(db *gorm.DB, planID string) ([]model.VPNServer, error) {
	var servers []model.VPNServer
	err := db.
		Joins("JOIN plan_servers ps ON ps.server_id = vpn_servers.id").
		Where("ps.plan_id = ? AND vpn_servers.is_active = ?", planID, true).
		Order("vpn_servers.current_load ASC").
		Find(&servers).Error
	return servers, err
}

func IsServerAllowedForPlan(db *gorm.DB, planID, serverID string) (bool, error) {
	var n int64
	err := db.Table("plan_servers").
		Where("plan_id = ? AND server_id = ?", planID, serverID).
		Count(&n).Error
	return n > 0, err
}

func SetUserPlan(db *gorm.DB, userID, planID string, lavaContractID *string, expiresAt *time.Time) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var plan model.Plan
		if err := tx.Where("id = ?", planID).First(&plan).Error; err != nil {
			return err
		}
		updates := map[string]interface{}{
			"plan_id":                 planID,
			"subscription_tier":       plan.Code,
			"subscription_expires_at": expiresAt,
		}
		return tx.Model(&model.User{}).Where("id = ?", userID).Updates(updates).Error
	})
}
