package middleware

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// semver represents a parsed semantic version (major.minor.patch).
// Pre-release tags (-beta, -rc, etc.) are ignored — we only gate on the numeric core.
type semver struct {
	major, minor, patch int
}

// parseSemver parses a version string like "2.0.0" or "1.2.3-beta" into a semver.
// Returns ok=false if the string doesn't have at least a major number.
func parseSemver(v string) (semver, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return semver{}, false
	}
	// Drop any pre-release suffix
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 {
		return semver{}, false
	}
	var out semver
	var err error
	if out.major, err = strconv.Atoi(parts[0]); err != nil {
		return semver{}, false
	}
	if len(parts) >= 2 {
		if out.minor, err = strconv.Atoi(parts[1]); err != nil {
			return semver{}, false
		}
	}
	if len(parts) >= 3 {
		if out.patch, err = strconv.Atoi(parts[2]); err != nil {
			return semver{}, false
		}
	}
	return out, true
}

// less returns true if a < b.
func (a semver) less(b semver) bool {
	if a.major != b.major {
		return a.major < b.major
	}
	if a.minor != b.minor {
		return a.minor < b.minor
	}
	return a.patch < b.patch
}

// SkipRule narrows a version-gate bypass to a specific (method, path) pair
// so that, for example, "POST /api/v1/health" is still gated even though
// "GET /api/v1/health" is not. This avoids the surprising broad-bypass
// behaviour of matching by path alone.
//
// When Prefix is true, the rule matches any request whose path begins with
// Path (after trailing-slash normalisation). Prefix rules are intended for
// non-mobile route trees like /api/v1/admin/ that are entirely exempt from
// the mobile version gate. An empty Method on a Prefix rule matches any
// HTTP method; on an exact rule an empty Method will never match.
type SkipRule struct {
	Method string
	Path   string
	Prefix bool
}

// AppVersion returns middleware that enforces a minimum client version.
// Clients must send the X-App-Version header on every request. Requests whose
// version is missing, malformed, or below minVersion receive 426 Upgrade Required.
//
// SkipRules bypass the check either by exact (method, path) match or — when
// Prefix is true — by path prefix. Intended for:
//   - GET /health
//   - POST /webhook/lava (called by lava.top servers, not the app)
//   - POST /auth/admin-login, POST /auth/refresh (callable from the web admin
//     panel without the mobile header)
//   - prefix /api/v1/admin/ for the entire admin route tree (web panel only)
func AppVersion(minVersion string, logger *zap.Logger, skipRules ...SkipRule) fiber.Handler {
	minParsed, ok := parseSemver(minVersion)
	if !ok {
		// Misconfiguration — fail fast at startup rather than silently allowing all traffic.
		logger.Fatal("invalid MIN_APP_VERSION", zap.String("value", minVersion))
	}
	logger.Info("app version gate enabled", zap.String("min_version", minVersion))

	// normalisePath strips a single trailing slash so the skip set is
	// matched the same way regardless of whether a caller registered
	// "/health" or "/health/", and regardless of whether the request path
	// arrived with or without a slash.
	normalisePath := func(p string) string {
		if len(p) > 1 && p[len(p)-1] == '/' {
			return p[:len(p)-1]
		}
		return p
	}

	type key struct{ method, path string }
	skipSet := make(map[key]struct{}, len(skipRules))
	// Prefix rules are scanned linearly after the exact-match lookup misses.
	// Keep them small (single-digit count expected) — any growth here should
	// be reviewed because prefix matching is the broader, more dangerous tool.
	type prefixRule struct{ method, path string }
	var prefixRules []prefixRule
	for _, r := range skipRules {
		if r.Prefix {
			// Ensure prefix ends with a slash so "/admin/" matches "/admin/users"
			// but does not accidentally match a sibling "/adminx".
			p := r.Path
			if len(p) == 0 || p[len(p)-1] != '/' {
				p += "/"
			}
			prefixRules = append(prefixRules, prefixRule{r.Method, p})
			continue
		}
		skipSet[key{r.Method, normalisePath(r.Path)}] = struct{}{}
	}

	return func(c *fiber.Ctx) error {
		if _, skip := skipSet[key{c.Method(), normalisePath(c.Path())}]; skip {
			return c.Next()
		}
		reqPath := c.Path()
		// Prefix match must compare against the un-normalised path so that
		// "/api/v1/admin/users" clearly starts with "/api/v1/admin/" — a
		// trailing slash is required on the rule side (enforced above).
		for _, pr := range prefixRules {
			if pr.method != "" && pr.method != c.Method() {
				continue
			}
			if strings.HasPrefix(reqPath, pr.path) {
				return c.Next()
			}
		}

		raw := c.Get("X-App-Version")
		if raw == "" {
			return c.Status(fiber.StatusUpgradeRequired).JSON(fiber.Map{
				"error":       "app_version_required",
				"message":     "Please update the app to continue",
				"min_version": minVersion,
			})
		}

		clientVer, ok := parseSemver(raw)
		if !ok {
			return c.Status(fiber.StatusUpgradeRequired).JSON(fiber.Map{
				"error":       "app_version_invalid",
				"message":     "Please update the app to continue",
				"min_version": minVersion,
			})
		}

		if clientVer.less(minParsed) {
			return c.Status(fiber.StatusUpgradeRequired).JSON(fiber.Map{
				"error":       "app_version_outdated",
				"message":     "Please update the app to continue",
				"min_version": minVersion,
			})
		}

		return c.Next()
	}
}
