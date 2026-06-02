package model

import "time"

// UserVlessIdentity is a per-user VLESS client UUID the tunnel admits at the
// wire (HARD-02, migration 026, D-06/D-07).
//
// Every user owns at most one ACTIVE identity per the repo's allocation
// discipline (GetOrCreateActiveVlessUUID inserts exactly one active row, rotation
// flips the old to is_active=false before inserting the next). The tunnel pulls
// the set of all active VlessUUID values and admits only those — so a revoked
// (is_active=false) UUID stops being admitted at the next reload, and two users
// never share a wire identity (VlessUUID is UNIQUE in the schema).
//
// VlessUUID is a random UUIDv4 (D-06: random-in-DB, no deterministic HMAC) so it
// is unguessable and unlinkable across users. RevokedAt is set when IsActive
// flips to false during rotation or admin revoke-all.
type UserVlessIdentity struct {
	ID        string     `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	UserID    string     `json:"user_id" gorm:"column:user_id;type:uuid;not null;index"`
	VlessUUID string     `json:"vless_uuid" gorm:"column:vless_uuid;type:uuid;not null;uniqueIndex"`
	IsActive  bool       `json:"is_active" gorm:"column:is_active;not null;default:true"`
	CreatedAt time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	RevokedAt *time.Time `json:"revoked_at" gorm:"column:revoked_at"`
}

// TableName pins the GORM table name to the migration-026 table so the default
// pluralizer ("user_vless_identities") can never drift from the DDL.
func (UserVlessIdentity) TableName() string {
	return "user_vless_identities"
}
