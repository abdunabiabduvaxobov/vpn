// Plain-Node entrypoint for the mock backend used by the Playwright suite.
// Pure CommonJS so it runs without ts-node / a build step (the standalone
// Next.js bundle has already been built before Playwright starts).
//
// Listens on http://127.0.0.1:4555 — Playwright config passes that URL as
// BACKEND_API_URL to the landing-node webServer entry.
//
// Endpoints documented in detail inside the handlers below. Keep behaviour
// dumb: deterministic responses + a /__set_invoice test-control endpoint
// for sequencing the polling state machine.

"use strict";

const http = require("node:http");

const PORT = Number(process.env.MOCK_BACKEND_PORT || 4555);

const invoices = new Map(); // id -> { sequence: string[], pollIndex: number }

function base64url(input) {
  return Buffer.from(input, "utf8")
    .toString("base64")
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

function buildMockJwt(payload) {
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const nowSec = Math.floor(Date.now() / 1000);
  const body = base64url(
    JSON.stringify({ iat: nowSec, exp: nowSec + 300, ...payload }),
  );
  return `${header}.${body}.mocksig`;
}

function send(res, status, body) {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json");
  res.end(typeof body === "string" ? body : JSON.stringify(body));
}

function readJson(req) {
  return new Promise((resolve) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => {
      const raw = Buffer.concat(chunks).toString("utf8");
      try {
        resolve(raw ? JSON.parse(raw) : {});
      } catch {
        resolve({});
      }
    });
  });
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url || "/", `http://localhost:${PORT}`);
  const path = url.pathname;
  const method = req.method || "GET";

  // ---- test-control endpoints ----
  if (path === "/__reset") {
    invoices.clear();
    return send(res, 200, { ok: true });
  }

  if (path === "/__set_invoice") {
    const body = await readJson(req);
    if (!body.id || !Array.isArray(body.sequence)) {
      return send(res, 400, { error: "bad_request" });
    }
    invoices.set(body.id, { sequence: body.sequence, pollIndex: 0 });
    return send(res, 200, { ok: true });
  }

  // ---- mocked Phase 2/3 backend ----
  if (path.startsWith("/api/v1/plans")) {
    return send(res, 200, {
      plans: [
        {
          code: "free",
          name: "Free",
          is_system: true,
          device_limit: 1,
          monthly_traffic_mb: 5120,
          server_countries: ["NL"],
          offers: [],
        },
        {
          code: "pro",
          name: "Pro",
          is_system: true,
          device_limit: 5,
          monthly_traffic_mb: 0,
          server_countries: ["NL", "DE", "US"],
          offers: [
            {
              period: "monthly",
              price: 4.99,
              currency: "USD",
              lava_offer_id: "uuid-monthly",
            },
          ],
        },
      ],
    });
  }

  if (path === "/api/v1/auth/apple" || path === "/api/v1/auth/google") {
    return send(res, 200, {
      access_token: buildMockJwt({ sub: "u1", plan_id: "free" }),
      refresh_token: "test_rt",
      expires_in: 300,
      user: { id: "u1", email: "alice@example.com", plan_id: "free" },
    });
  }

  if (path === "/api/v1/auth/refresh") {
    // B2 — return JWT carrying plan_id=pro so the Plan 03 proxy re-issues
    // rv_user with the upgraded plan after pay/success force-refresh.
    return send(res, 200, {
      access_token: buildMockJwt({ sub: "u1", plan_id: "pro" }),
      refresh_token: "rotated_rt",
      expires_in: 300,
    });
  }

  if (path === "/api/v1/auth/logout") {
    res.statusCode = 204;
    return res.end();
  }

  if (path === "/api/v1/checkout" && method === "POST") {
    return send(res, 200, {
      payment_url: "https://gate.lava.top/pay/abc",
      invoice_id: "inv-123",
    });
  }

  const invMatch = path.match(/^\/api\/v1\/invoices\/([^/?]+)/);
  if (invMatch) {
    const id = invMatch[1];
    const state = invoices.get(id);
    const status = state
      ? state.sequence[Math.min(state.pollIndex, state.sequence.length - 1)]
      : "pending";
    if (state) state.pollIndex += 1;
    return send(res, 200, { id, status, plan_code: "pro" });
  }

  send(res, 404, { error: "mock_not_found", path });
});

server.listen(PORT, "127.0.0.1", () => {
  process.stdout.write(
    `mock-backend listening on http://127.0.0.1:${PORT}\n`,
  );
});
