package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// resetAuth lets each case set its own token. adminToken() memoises via sync.Once, which
// is right in production and useless in a test, so the tests write the cached value
// directly rather than fighting the Once.
func resetAuth(t *testing.T, token string) {
	t.Helper()
	adminTokenOnce.Do(func() {}) // burn the Once so it never re-reads the environment
	adminTokenVal = token
	t.Cleanup(func() { adminTokenVal = "" })
}

func originReq(origin, host string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://"+host+"/ws", nil)
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

// The regression this whole file exists for: a page on an unrelated origin must not be
// able to open a control socket. Browsers do not enforce same-origin on WebSockets, so
// if this test ever passes trivially again, the building is remotely switchable by any
// site an operator happens to visit.
func TestCheckOriginRejectsForeignOrigin(t *testing.T) {
	t.Setenv("ECON_ALLOWED_ORIGINS", "")
	cases := []struct {
		name, origin, host string
	}{
		{"attacker site", "https://evil.example.com", "10.0.0.5:8080"},
		{"lookalike prefix", "http://10.0.0.5.evil.com", "10.0.0.5:8080"},
		{"lookalike suffix", "http://evil-10.0.0.5", "10.0.0.5:8080"},
		{"null origin", "null", "10.0.0.5:8080"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if checkOrigin(originReq(c.origin, c.host)) {
				t.Fatalf("origin %q accepted against host %q — this is the CSRF hole", c.origin, c.host)
			}
		})
	}
}

// The legitimate deployments must keep working, or whoever hits the failure will simply
// restore `return true` and undo the fix.
func TestCheckOriginAcceptsLegitimateClients(t *testing.T) {
	t.Setenv("ECON_ALLOWED_ORIGINS", "")
	cases := []struct {
		name, origin, host string
	}{
		{"vite dev server", "http://localhost:5173", "localhost:8080"},
		{"loopback ip", "http://127.0.0.1:5173", "127.0.0.1:8080"},
		{"phone on the LAN", "http://192.168.1.254:5173", "192.168.1.254:8080"},
		{"built dashboard same host", "http://192.168.1.254", "192.168.1.254:8080"},
		{"non-browser client sends no Origin", "", "10.0.0.5:8080"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !checkOrigin(originReq(c.origin, c.host)) {
				t.Fatalf("legitimate origin %q rejected against host %q", c.origin, c.host)
			}
		})
	}
}

func TestCheckOriginHonoursAllowlist(t *testing.T) {
	t.Setenv("ECON_ALLOWED_ORIGINS", "https://twin.example.com, https://ops.example.com")
	if !checkOrigin(originReq("https://twin.example.com", "10.0.0.5:8080")) {
		t.Fatal("allowlisted origin rejected")
	}
	if checkOrigin(originReq("https://other.example.com", "10.0.0.5:8080")) {
		t.Fatal("non-allowlisted origin accepted")
	}
}

func TestParseAuthMessage(t *testing.T) {
	cases := []struct {
		in       string
		wantTok  string
		wantIsAu bool
	}{
		{`{"action":"auth","token":"s3cret"}`, "s3cret", true},
		{`{"action":"auth","token":""}`, "", true},
		{`{"action":"LIGHTS_OFF","zone":"z1"}`, "", false},
		{`{"action":"autopilot","value":false}`, "", false},
		{`SCENARIO_HEATWAVE`, "", false},
		{``, "", false},
		{`{not json`, "", false},
	}
	for _, c := range cases {
		tok, isAuth := parseAuthMessage([]byte(c.in))
		if tok != c.wantTok || isAuth != c.wantIsAu {
			t.Errorf("parseAuthMessage(%q) = (%q,%v), want (%q,%v)", c.in, tok, isAuth, c.wantTok, c.wantIsAu)
		}
	}
}

// An empty token must not authorize when a token IS configured. This is the failure mode
// where a client that never authenticated is treated as authenticated because both sides
// compared "" to "".
func TestEmptyTokenNeverAuthorizesWhenEnforced(t *testing.T) {
	resetAuth(t, "the-real-token")
	if !authEnforced() {
		t.Fatal("auth should be enforced when a token is set")
	}
	if tokenMatches("") {
		t.Fatal("empty token accepted while enforcing")
	}
	if tokenMatches("wrong") {
		t.Fatal("wrong token accepted")
	}
	if !tokenMatches("the-real-token") {
		t.Fatal("correct token rejected")
	}
}

func TestDemoModeAcceptsAnyToken(t *testing.T) {
	resetAuth(t, "")
	if authEnforced() {
		t.Fatal("auth should not be enforced with no token set")
	}
	if !tokenMatches("") || !tokenMatches("anything") {
		t.Fatal("demo mode should accept anything — it is documented as open")
	}
}

// requireAdmin is the REST half of the same gate; it must answer 401 rather than
// silently proceeding, and must share auth.go's token so the two surfaces cannot drift.
func TestRequireAdminGatesRESTWrites(t *testing.T) {
	resetAuth(t, "tok")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/building", nil)
	if requireAdmin(w, r) {
		t.Fatal("requireAdmin passed a request with no token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/building", nil)
	r.Header.Set("X-Admin-Token", "tok")
	if !requireAdmin(w, r) {
		t.Fatal("requireAdmin rejected the correct token")
	}
}
