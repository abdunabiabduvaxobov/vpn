# Phase 8: Cleanup & hardening - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-02
**Phase:** 08-cleanup-hardening
**Areas discussed:** Per-user VLESS UUID enforcement, Refresh-token cutover + binding, Mobile secure-storage migration, Stripe removal + migration scope

---

## Per-user VLESS UUID enforcement (HARD-02)

### Enforcement depth
| Option | Description | Selected |
|--------|-------------|----------|
| API-only (meets SC, lighter) | Return + rotate per-user UUID; Xray still accepts shared UUID | |
| Full Xray enforcement | API manages UUID set AND syncs to Xray so tunnel rejects unknown/revoked | ✓ |
| Full enforcement, separate phase | API-only now, split heavy Xray work to a follow-up phase | |

**User's choice:** Full Xray enforcement.

### Sync method to Xray
| Option | Description | Selected |
|--------|-------------|----------|
| Xray gRPC HandlerService | Live AddUser/RemoveUser, no restart | |
| Full config regen + reload | Regenerate whole Xray JSON with active-UUID list, reload | ✓ |
| You decide (planner picks) | Lock requirement, planner chooses | |

**User's choice:** Full config regen + reload.

### UUID derivation/tracking
| Option | Description | Selected |
|--------|-------------|----------|
| Random, stored in DB | Random UUIDv4 per user in a registry table | |
| Deterministic HMAC | HMAC(secret, user_id+epoch) | |
| You decide (planner picks) | Lock behavior, planner chooses | ✓ |

**User's choice:** Planner's discretion.

### Rotation timing on plan change
| Option | Description | Selected |
|--------|-------------|----------|
| Immediate revoke | Old UUID dropped at once, brief reconnect blip | |
| Grace window | Old UUID valid ~5 min after rotation | |
| You decide (planner picks) | Lock behavior, planner chooses | ✓ |

**User's choice:** Planner's discretion.

---

## Refresh-token cutover + binding (HARD-03 / HARD-04)

### Cutover handling
| Option | Description | Selected |
|--------|-------------|----------|
| Force re-login (clean break) | All existing sessions invalid at deploy; one re-login | ✓ |
| Dual-read grace window | Accept old JWT + new opaque during a window | |

**User's choice:** Force re-login (clean break).
**Notes:** Justified by zero paying users + "free hand to break things"; avoids keeping the JWT-refresh code path S1-2 wants removed.

### IP binding strictness
| Option | Description | Selected |
|--------|-------------|----------|
| device_id hard, IP log-only | Reject on device_id change; log IP mismatch but allow | ✓ |
| device_id + IP both reject | Reject on either change | |
| device_id hard, IP /24 reject | Reject on device_id; IP reject only on /24 prefix change | |

**User's choice:** device_id hard reject, IP log-only.
**Notes:** Chosen to avoid false logouts from mobile cell↔wifi roaming; matches audit literal wording.

---

## Mobile secure-storage migration (HARD-16)

### Library
| Option | Description | Selected |
|--------|-------------|----------|
| react-native-keychain | iOS Keychain + Android EncryptedSharedPreferences directly | |
| MMKV with encryption | Reuse existing MMKV dep; does not satisfy Xcode-Keychain SC alone | |
| You decide (planner picks) | Lock requirement, planner chooses | ✓ |

**User's choice:** Planner's discretion (locked: tokens in Keychain/EncryptedSharedPreferences, Xcode-verifiable).

### Migration of existing AsyncStorage tokens
| Option | Description | Selected |
|--------|-------------|----------|
| Migrate then wipe | Copy to secure store on first launch, delete from AsyncStorage | |
| Force re-login (clean) | Clear AsyncStorage tokens, user re-auths once | |
| You decide (planner picks) | Lock requirement, planner chooses | ✓ |

**User's choice:** Planner's discretion (locked: AsyncStorage ends with no tokens). Note: backend clean-break cutover (D-09) forces a re-login regardless — coordinate.

---

## Stripe removal + migration scope (HARD-01)

### Migration approach
| Option | Description | Selected |
|--------|-------------|----------|
| No new migration, verify + document | Rely on mig 020 (already dropped); add a verification step | |
| Add idempotent no-op migration | Redundant DROP COLUMN IF EXISTS in a Phase 8 file | |
| You decide (planner picks) | Lock column-absent, planner chooses | ✓ |

**User's choice:** Planner's discretion (locked: column verifiably absent).
**Notes:** Confirmed `subscriptions.stripe_id` already dropped in `migrations/020_lava_payments.sql:85`.

### Stripe test fixture disposition
| Option | Description | Selected |
|--------|-------------|----------|
| Delete Stripe tests outright | Remove Stripe test cases + stripe-go imports | ✓ |
| Rewrite against lava | Port covered behavior to lava handler tests | |
| You decide (planner picks) | Lock no stripe-go in go.mod/tests | |

**User's choice:** Delete Stripe tests outright.
**Notes:** Stripe handlers gone since Phase 3; lava path covered by webhook_lava_test.go.

## Claude's Discretion

- VLESS UUID derivation & rotation timing (HARD-02)
- Mobile secure-storage library & migration path (HARD-16)
- Stripe verify-only vs redundant migration (HARD-01)
- Admin CSP exact policy (HARD-08)
- useVpnConnection hook decomposition shape (HARD-15)

## Deferred Ideas

- Full VLESS enforcement as a separate phase — rejected (user chose full enforcement in Phase 8).
- Xray gRPC HandlerService — deferred in favor of config regen+reload.
- Sentry/external error sink — v2 (MUX-01).
- Non-HARD audit items (logout endpoint S1-6, BodyLimit S6-1, CORS risevpn.com S8-1) — close only if planner finds them in the four reports' scope.
