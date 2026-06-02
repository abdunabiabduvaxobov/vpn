# SC#5 — Auth token in OS Keychain, absent from AsyncStorage (HARD-16)

> **Manual verification** — Keychain entries are only inspectable through Xcode /
> Keychain Access (iOS) and the EncryptedSharedPreferences XML (Android); they
> cannot be asserted from jest. Run this at the Phase 8 gate after the HARD-16
> secure-storage swap lands.

## What this proves

After HARD-16, the auth token pair is stored via **`react-native-keychain`**
(iOS Keychain / Android EncryptedSharedPreferences) and **never** in AsyncStorage.
Before HARD-16, `authStore.ts` wrote the pair to
`AsyncStorage.setItem('auth-tokens', ...)` at 7 sites — a plaintext, sandbox-file
store. SC#5 confirms the token now lives in the OS secure store and that the old
`auth-tokens` AsyncStorage key holds **no** token.

## Load-bearing identifiers

| Thing | Value |
|-------|-------|
| App display name | **Rise VPN** |
| iOS bundle id / Android applicationId | **`com.vpnapp`** |
| Old AsyncStorage token key (must be ABSENT/empty) | **`auth-tokens`** |
| Keychain service (default for `setGenericPassword`) | the app bundle id, **`com.vpnapp`** (confirm the exact `service` string in `secureTokenStore.ts` once HARD-16 lands; if it passes an explicit `{ service }` option, look for that string instead) |

---

## iOS (Simulator or device via Xcode)

1. **Build & run** the app to the iOS Simulator (or a connected device):
   ```sh
   cd app
   npx react-native run-ios          # or build/run from Xcode (VpnApp scheme)
   ```
2. **Sign in** in the app (Apple, Google, or guest — any path that mints tokens).
3. **Confirm the token IS in the Keychain:**
   - Simulator: the simulator shares the host login Keychain for generic
     passwords. Open **Keychain Access** (macOS) → search for **`com.vpnapp`**
     (or the explicit service name). You should see a **generic password** entry
     created by the app. Double-click → "Show password" → it is the token JSON
     pair.
   - Device: **Xcode → Window → Devices and Simulators → select device → the app
     → "Download Container"**, or inspect via a Keychain dump tool. The generic
     password item for service `com.vpnapp` must exist.
4. **Confirm the token is NOT in AsyncStorage:**
   - Locate the app's AsyncStorage backing file. RN's `RCTAsyncLocalStorage`
     persists to a manifest under the app container:
     ```
     <App container>/Library/Application Support/<bundle>/RCTAsyncLocalStorage_V1/manifest.json
     ```
     On the Simulator the container is under:
     ```
     ~/Library/Developer/CoreSimulator/Devices/<DEVICE-UDID>/data/Containers/Data/Application/<APP-UUID>/Library/Application Support/RCTAsyncLocalStorage_V1/manifest.json
     ```
     (find `<APP-UUID>` with `xcrun simctl get_app_container booted com.vpnapp data`).
   - **Open `manifest.json` and confirm there is NO `auth-tokens` key** (and no
     token value in any spilled `*.json` entry file alongside it). A one-time
     boot cleanup (`AsyncStorage.removeItem('auth-tokens')`) means even an
     upgrade-in-place user ends with no token here.

**iOS PASS:** generic password for `com.vpnapp` exists in Keychain **AND**
`auth-tokens` is absent from the AsyncStorage manifest.

---

## Android (Emulator or device)

1. **Build & run:**
   ```sh
   cd app
   npx react-native run-android
   ```
2. **Sign in** in the app.
3. **Confirm the token IS in EncryptedSharedPreferences:**
   - `react-native-keychain` on Android writes to a Keystore-backed
     **EncryptedSharedPreferences** XML:
     ```sh
     adb shell run-as com.vpnapp ls shared_prefs
     # expect an encrypted prefs xml (e.g. com.vpnapp.<keychain-prefs>.xml)
     adb shell run-as com.vpnapp cat shared_prefs/<that-file>.xml
     ```
     The values are **encrypted** (Keystore AES) — you will see ciphertext, not
     the raw token. That the file exists and is encrypted is the pass signal.
4. **Confirm the token is NOT in AsyncStorage:**
   - RN AsyncStorage on Android uses a SQLite DB (`RKStorage`):
     ```sh
     adb shell run-as com.vpnapp ls databases        # expect RKStorage
     adb shell run-as com.vpnapp \
       sqlite3 databases/RKStorage "SELECT key FROM catalystLocalStorage;"
     ```
   - **Confirm `auth-tokens` is NOT among the returned keys** (and no token value
     in any row).

**Android PASS:** the encrypted prefs XML exists **AND** `auth-tokens` is absent
from `RKStorage`.

---

> **Implementation note (08-09 landed):** `secureTokenStore.ts` passes an
> **explicit** `{ service: 'risevpn.auth' }` to every
> `setGenericPassword` / `getGenericPassword` / `resetGenericPassword` call. So
> the load-bearing identifier to search for is **`risevpn.auth`**, NOT the
> default bundle id. On iOS search Keychain Access for `risevpn.auth`; on
> Android the encrypted prefs file is named after that service.

---

## Coordinated single re-login (D-09 ↔ D-12, Open Risk 1)

HARD-16 (this plan, 08-09) wipes the mobile AsyncStorage token AND HARD-03/04
(plan 08-04) clears `sessions` server-side. If these ship on different days the
user re-logs in **twice**. They MUST ship as **one release wave** so the user
re-authenticates exactly **once**.

**Release sequence (runbook step, not just code):**

1. Deploy the 08-04 backend cutover (migration that does `DELETE FROM sessions`;
   opaque device-bound refresh tokens live). All existing tokens are now dead
   server-side.
2. Release the new mobile build (this plan) at the same time.
3. On first launch of the new build:
   - `initialize()` runs a one-time `AsyncStorage.removeItem('auth-tokens')`.
   - `secureTokenStore.getTokens()` finds nothing (fresh Keychain) → the app
     re-mints a guest / routes to login.
   - The fresh tokens are written **straight to the Keychain** (never AsyncStorage).

**Single-re-login verification:**

- [ ] With the 08-04 cutover deployed, launch the new build on a device that was
      previously signed in on the OLD build.
- [ ] Confirm the user is asked to authenticate **exactly once** (a guest auto
      re-mint counts as the one event; an Apple/Google user signs in once).
- [ ] Confirm Pro/guest tier is correct afterwards (no downgrade, no double prompt).
- [ ] Confirm a subsequent token refresh succeeds — the `/auth/refresh` request
      body now carries `device_id` (HARD-04); the backend must accept it.

## device_id on refresh (HARD-04 client side)

`services/api.ts` now sends `device_id` (from `getDeviceFingerprint()`) in the
`/auth/refresh` body so the backend can hard-reject a refresh token replayed
from a different device.

**How to confirm on device (optional, network inspection):**

- [ ] Force an access-token expiry (wait out the 5-min TTL or clear the access
      token), trigger any authenticated call, and capture the `/auth/refresh`
      request. Confirm the JSON body contains both `refresh_token` and
      `device_id`.
- [ ] Confirm a refresh with the wrong/foreign `device_id` is rejected by the
      backend (cross-check with 08-04's device-binding behaviour).

---

## Overall SC#5 result

- [ ] iOS: token in Keychain (service **`risevpn.auth`**), `auth-tokens` absent from AsyncStorage manifest
- [ ] Android: encrypted prefs XML present, `auth-tokens` absent from `RKStorage`
- [ ] Single coordinated re-login confirmed (exactly one auth prompt at cutover)
- [ ] `/auth/refresh` body carries `device_id`

All boxes checked → **SC#5 + HARD-16 PASS**. Attach a screenshot of the Keychain
entry, the grep showing no `auth-tokens` key, and a note confirming the single
re-login to the phase verification record.
