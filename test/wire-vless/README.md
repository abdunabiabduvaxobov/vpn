# Wire-level VLESS rejection harness (HARD-02 / SC#2)

Proves **at the wire** that the RiseVPN tunnel admits **only** the UUIDs in its
active client set and **rejects** every foreign or revoked UUID. When a foreign
UUID is presented, xray-core's REALITY transport falls back to the impersonated
destination (`www.microsoft.com`) and **no VLESS session is established** — so a
client configured with that UUID cannot tunnel any traffic.

This is the heaviest validation in Phase 8. It is run **manually at the phase
gate** and is **not** part of `go test`.

> **SC#2 is proven by Step 4** (foreign UUID → rejection). Step 3 is the positive
> control (active UUID → success); without it a Step-4 failure could be a setup
> bug rather than a genuine rejection.

---

## What's in here

| File | Role |
|------|------|
| `docker-compose.yml` | Brings up the real tunnel + a scripted `xray` VLESS client on a private bridge network. |
| `tunnel-config.json` | The tunnel's config. Its `clients` array is the **active set `{U1}`** = `11111111-1111-4111-8111-111111111111`. REALITY uses the example/test keys — **never production**. |
| `client-good-uuid.json` | Positive control. Presents **U1** (in the active set). Expected: handshake **succeeds**. |
| `client-foreign-uuid.json` | The proof. Presents **U2** = `22222222-2222-4222-8222-222222222222` (NOT in the active set). Everything else is identical to the good config. Expected (post-HARD-02): **rejected**. |

`U1`, `U2`, and the REALITY public key (`OAmaJn5J...`), `serverName`
(`www.microsoft.com`), and `shortId` (`abcd1234`) are the load-bearing values.
The good and foreign client configs differ in **exactly one field: the VLESS
`id`**. That isolation is what makes a Step-4 rejection attributable to the UUID
and nothing else.

---

## Prerequisites

- Docker + Docker Compose v2.
- Run all commands from `test/wire-vless/`:
  ```sh
  cd test/wire-vless
  ```

---

## Step 1 — Start the tunnel with active set {U1}

```sh
docker compose up -d --build tunnel
docker compose logs -f tunnel   # wait for "starting xray-core" / health on :8080
```

Confirm the tunnel is healthy:

```sh
curl -sf http://localhost:8080/      # tunnel health endpoint → 200
```

## Step 2 — (sanity) confirm the active set

The tunnel admits exactly `tunnel-config.json -> clients`. Verify it is `{U1}`:

```sh
grep -A2 '"clients"' tunnel-config.json
# clients: ["11111111-1111-4111-8111-111111111111"]
```

## Step 3 — POSITIVE control: present U1 → handshake SUCCEEDS

Run the client with the **good** config (U1, which is in the active set):

```sh
docker compose run --rm \
  --entrypoint /usr/bin/xray xray-client \
  -config /etc/xray/client-good-uuid.json &

# In the client container the SOCKS proxy is on :10808. Tunnel a request:
docker compose exec xray-client \
  sh -c 'curl -s --socks5-hostname 127.0.0.1:10808 https://www.gstatic.com/generate_204 -o /dev/null -w "%{http_code}\n"'
```

**Expected:** `204` — traffic tunnels through. The VLESS handshake with U1
succeeded. (If this fails, the REALITY keys / serverName / shortId are
mismatched — fix the setup before trusting Step 4.)

## Step 4 — THE PROOF: present U2 (foreign/revoked) → REJECTED

Run the client with the **foreign** config (U2, NOT in the active set):

```sh
docker compose run --rm \
  --entrypoint /usr/bin/xray xray-client \
  -config /etc/xray/client-foreign-uuid.json &

docker compose exec xray-client \
  sh -c 'curl -s --max-time 10 --socks5-hostname 127.0.0.1:10808 https://www.gstatic.com/generate_204 -o /dev/null -w "%{http_code}\n" || echo "TUNNEL FAILED (expected)"'
```

**Expected (after HARD-02 lands):** the curl fails / times out
(`TUNNEL FAILED (expected)`), and the tunnel log shows REALITY falling back to
the impersonated dest with **no** VLESS session created for U2. **This is SC#2.**

> **Before HARD-02 lands** the tunnel still admits a single shared UUID
> (`servers.go` returns `cfg.TunnelVLESSUUID` for everyone), so a "foreign" UUID
> only differs if it is genuinely not the shared one. To observe the CURRENT
> (insecure) behaviour, temporarily set `client-foreign-uuid.json`'s `id` to the
> deployment's shared `TUNNEL_VLESS_UUID` and watch it ACCEPT — then, after
> HARD-02 allocates per-user UUIDs, restore U2 to a value the tunnel never
> activated and confirm it now REJECTS. The pass condition for the phase gate is
> the **post-HARD-02 reject**.

## Step 5 — Tear down

```sh
docker compose down -v
```

---

## Pass / fail summary

| Step | Presents | Pass condition |
|------|----------|----------------|
| 3 | U1 (active) | `204` — tunnel established (positive control) |
| 4 | U2 (foreign/revoked) | curl fails/times out; tunnel log shows REALITY fallback, no VLESS session — **proves SC#2** |
