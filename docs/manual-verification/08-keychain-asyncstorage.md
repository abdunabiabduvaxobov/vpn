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

## Overall SC#5 result

- [ ] iOS: token in Keychain (service `com.vpnapp`), `auth-tokens` absent from AsyncStorage manifest
- [ ] Android: encrypted prefs XML present, `auth-tokens` absent from `RKStorage`

Both boxes checked → **SC#5 PASS**. Attach a screenshot of the Keychain entry
and the grep showing no `auth-tokens` key to the phase verification record.
