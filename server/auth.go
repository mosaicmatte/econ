package main

// Access control for the two surfaces that can move a relay.
//
// The REST surface was already gated (requireAdmin in blueprint.go). The WebSocket was
// not, and it is the one that matters most: /ws accepts override commands that travel
// econ/commands/<topic> -> ESP32 -> opto-isolated relay -> 220 V. Two separate holes
// existed there.
//
//  1. Upgrader.CheckOrigin returned true unconditionally. Browsers do NOT apply the
//     same-origin policy to WebSockets — CheckOrigin is the entire defence — so any page
//     an operator visited while on the building network could open a socket to the engine
//     and switch the building. This is the WebSocket form of CSRF, and it needs no
//     credentials to work because the connection carries none.
//
//  2. Once connected, every client could command every zone. Telemetry and control shared
//     one unauthenticated channel.
//
// The fix keeps the project's existing convention: ECON_ADMIN_TOKEN unset means demo mode
// (open, and the engine says so loudly at boot); set means enforced. What is NOT
// conditional is the origin check — that one is always on, because a permissive default
// there cannot be justified by convenience: the dashboard passes it in every legitimate
// deployment, including the phone-on-the-LAN case, since it compares hostnames rather
// than full origins.

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

// adminToken is read once. Empty means the deployment has not set one.
var (
	adminTokenOnce sync.Once
	adminTokenVal  string
)

func adminToken() string {
	adminTokenOnce.Do(func() {
		adminTokenVal = os.Getenv("ECON_ADMIN_TOKEN")
	})
	return adminTokenVal
}

// authEnforced reports whether write operations require a token. When false the engine
// is in demo mode and anyone who can reach it can control it.
func authEnforced() bool { return adminToken() != "" }

// tokenMatches compares in constant time so a wrong token cannot be discovered a byte at
// a time by measuring how long the comparison took.
func tokenMatches(got string) bool {
	want := adminToken()
	if want == "" {
		return true // demo mode: nothing to match against
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// allowedOrigins is the optional explicit allowlist, ECON_ALLOWED_ORIGINS, comma
// separated (e.g. "https://twin.example.com,https://ops.example.com"). It is only needed
// when the dashboard is served from a DIFFERENT host than the engine; same-host
// deployments pass on the hostname rule below without configuration.
func allowedOrigins() []string {
	raw := os.Getenv("ECON_ALLOWED_ORIGINS")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.ToLower(p))
		}
	}
	return out
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

// checkOrigin decides whether a WebSocket upgrade may proceed.
//
// Absent Origin header: allowed. Only browsers send Origin, and only browsers are
// subject to the attack this defends against — a native client, curl or an edge node
// gains nothing by lying here, because it could talk to the MQTT broker directly. The
// header's job is to stop a *page* the operator did not intend to grant control to.
//
// Present Origin: the hostname must match the request Host, be an explicit allowlist
// entry, or be loopback. Comparing hostnames rather than whole origins is deliberate:
// in every normal deployment the dashboard is served on :5173 (dev) or :80 (built)
// while the engine listens on :8080, so a strict origin==host test would reject the
// legitimate case and push whoever hit it straight back to returning true.
func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		log.Printf("[auth] rejected websocket: unparseable Origin %q", origin)
		return false
	}
	oh := strings.ToLower(u.Hostname())

	if isLoopback(oh) {
		return true
	}
	// Same machine as the engine, whatever port the page came from.
	if rh := strings.ToLower(r.Host); rh != "" {
		if h := rh; h == oh {
			return true
		}
		if i := strings.LastIndex(rh, ":"); i > 0 && strings.EqualFold(rh[:i], oh) {
			return true
		}
	}
	for _, a := range allowedOrigins() {
		if a == "*" {
			return true
		}
		if a == strings.ToLower(origin) || a == oh {
			return true
		}
	}
	log.Printf("[auth] rejected websocket from Origin %q (host %q). "+
		"If this is a legitimate deployment, add it to ECON_ALLOWED_ORIGINS.", origin, r.Host)
	return false
}

// parseAuthMessage recognises the control handshake {"action":"auth","token":"..."} and
// returns the token. The second return distinguishes "this was an auth message" from
// "this was an auth message with an empty token", which must not be treated as a pass.
func parseAuthMessage(msg []byte) (string, bool) {
	trimmed := strings.TrimSpace(string(msg))
	if trimmed == "" || trimmed[0] != '{' {
		return "", false
	}
	var m struct {
		Action string `json:"action"`
		Token  string `json:"token"`
	}
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		return "", false
	}
	if m.Action != "auth" {
		return "", false
	}
	return m.Token, true
}

// logAuthPosture prints what the engine will and will not accept, once, at boot. A
// deployment that believes it is secured and is not should find that out from its own
// logs rather than from an incident.
func logAuthPosture() {
	if authEnforced() {
		log.Println("[auth] write operations require ECON_ADMIN_TOKEN (REST: X-Admin-Token header; WS: auth message)")
	} else {
		log.Println("[auth] WARNING: ECON_ADMIN_TOKEN is not set — anyone who can reach this engine can control the building. " +
			"Set it before connecting real hardware.")
	}
	if o := allowedOrigins(); len(o) > 0 {
		log.Printf("[auth] extra allowed websocket origins: %v", o)
	}
}
