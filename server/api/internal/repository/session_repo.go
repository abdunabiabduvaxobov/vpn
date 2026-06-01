package repository

import (
	"context"
	"errors"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// CreateSession stores a new refresh token session.
func CreateSession(ctx context.Context, db *gorm.DB, session *model.Session) error {
	return db.WithContext(ctx).Create(session).Error
}

// FindSessionByTokenHash finds a valid (non-expired) session by refresh token hash.
func FindSessionByTokenHash(ctx context.Context, db *gorm.DB, tokenHash string) (*model.Session, error) {
	var session model.Session
	result := db.WithContext(ctx).Where("refresh_token_hash = ? AND expires_at > ?", tokenHash, time.Now()).First(&session)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &session, nil
}

// DeleteSession removes a session by ID (used during token refresh).
func DeleteSession(ctx context.Context, db *gorm.DB, id string) error {
	return db.WithContext(ctx).Delete(&model.Session{}, "id = ?", id).Error
}

// DeleteExpiredSessions removes all sessions past their expiry time.
func DeleteExpiredSessions(ctx context.Context, db *gorm.DB) (int64, error) {
	result := db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&model.Session{})
	return result.RowsAffected, result.Error
}

// DeleteUserSessions deletes every refresh-session row for the given user.
//
// Called by the logout handler (plan 06) per the Discretion default in
// CONTEXT.md "Logout request body" — "delete ALL sessions for this user"
// (RESEARCH.md §Open Question #1 recommendation (a)). Returns the count of
// deleted rows so the handler can log how many sessions were terminated.
//
// Indexed by idx_sessions_user_id (created in earlier migrations). A user
// with N active sessions sees one DELETE … WHERE user_id = $1 issued.
func DeleteUserSessions(ctx context.Context, db *gorm.DB, userID string) (int64, error) {
	result := db.WithContext(ctx).Where("user_id = ?", userID).Delete(&model.Session{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// ListSessionsByUser returns every refresh-session row for the given user,
// newest first. Read-only — backs the admin panel's per-user "active sessions"
// card (ADMIN-02). Indexed by idx_sessions_user_id.
func ListSessionsByUser(ctx context.Context, db *gorm.DB, userID string) ([]model.Session, error) {
	var sessions []model.Session
	result := db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&sessions)
	if result.Error != nil {
		return nil, result.Error
	}
	return sessions, nil
}
