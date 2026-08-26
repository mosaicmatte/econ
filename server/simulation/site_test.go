package simulation

import (
	"os"
	"testing"
)

// The building id says which building the learned state DESCRIBES. It cannot say whether
// the engine is currently AT that building — the fixture travels with the machine, so a
// laptop running the house's building-data.local.json somewhere else passes every other
// provenance check and folds that somewhere-else into the house's learned normal.

func TestFingerprintIsStableAndOpaque(t *testing.T) {
	a := hashSite("bc:fc:e7:bf:33:54", "192.168.50.0/24")
	b := hashSite("bc:fc:e7:bf:33:54", "192.168.50.0/24")
	if a != b {
		t.Errorf("same network hashed two ways: %q vs %q", a, b)
	}
	if a == "" || len(a) != 16 {
		t.Errorf("fingerprint should be 16 hex chars, got %q", a)
	}
	// The router's hardware address must not be recoverable from, or visible in, anything
	// that gets written to a state file or a log line.
	if got := hashSite("bc:fc:e7:bf:33:54", "192.168.50.0/24"); got == "bc:fc:e7:bf:33:54" ||
		len(got) != 16 {
		t.Error("fingerprint must be a hash, never the address itself")
	}
	if c := hashSite("bc:fc:e7:bf:33:55", "192.168.50.0/24"); c == a {
		t.Error("a different router must produce a different fingerprint")
	}
	// Two sites behind identically-cloned routers still separate on their subnet.
	if d := hashSite("bc:fc:e7:bf:33:54", "10.0.0.0/24"); d == a {
		t.Error("a different subnet must produce a different fingerprint")
	}
}

func TestMacNormalizationSurvivesPlatformFormatting(t *testing.T) {
	// macOS abbreviates a leading zero octet as "0" where Linux prints "00". The same
	// router must not read as two different sites depending on which host the engine runs
	// on, or on which of the two printed it.
	long := normalizeMac("BC:0F:E7:BF:33:54")
	short := normalizeMac("bc:f:e7:bf:33:54")
	if long != short {
		t.Errorf("%q and %q are the same address but normalized differently", long, short)
	}
	for _, bad := range []string{"", "incomplete", "not-a-mac", "bc:fc:e7:bf:33", "bc:fc:e7:bf:33:54:99"} {
		if got := normalizeMac(bad); got != "" {
			t.Errorf("normalizeMac(%q) should be empty, got %q", bad, got)
		}
	}
}

func TestAnUnknownSiteNeverDestroysLearnedState(t *testing.T) {
	// This is the rule that matters most. An empty fingerprint means "cannot tell", which
	// is NOT "somewhere else": state written before this check existed carries none, and an
	// engine with no route to a gateway can offer none. Neither is grounds for throwing
	// away hours of learning.
	if !sameSite("") {
		t.Error("state with no recorded site must still restore; it predates the check")
	}

	prev, had := os.LookupEnv("SITE_FINGERPRINT")
	t.Cleanup(func() {
		if had {
			os.Setenv("SITE_FINGERPRINT", prev)
		} else {
			os.Unsetenv("SITE_FINGERPRINT")
		}
	})

	// And with a known current site, a genuinely different one IS caught.
	os.Setenv("SITE_FINGERPRINT", "the-house")
	house := SiteFingerprint()
	os.Setenv("SITE_FINGERPRINT", "somewhere-else")
	elsewhere := SiteFingerprint()
	if house == elsewhere || house == "" {
		t.Fatalf("two declared sites should differ: %q vs %q", house, elsewhere)
	}
}

func TestDeclaredFingerprintOverridesDetection(t *testing.T) {
	// A deployment behind a router it does not control, or several machines that should
	// share one site identity, set SITE_FINGERPRINT and the network is not probed at all.
	prev, had := os.LookupEnv("SITE_FINGERPRINT")
	t.Cleanup(func() {
		if had {
			os.Setenv("SITE_FINGERPRINT", prev)
		} else {
			os.Unsetenv("SITE_FINGERPRINT")
		}
	})

	os.Setenv("SITE_FINGERPRINT", "hcmc-tube-house")
	a := SiteFingerprint()
	b := SiteFingerprint()
	if a == "" || a != b {
		t.Errorf("a declared site must be stable, got %q then %q", a, b)
	}
	if a == "hcmc-tube-house" {
		t.Error("even a declared site is stored hashed, not verbatim")
	}
}

func TestDetectionDegradesRatherThanFailing(t *testing.T) {
	// On a machine with a reachable gateway this resolves; on one without, it must return
	// empty rather than error out or block. Either way it must not panic, and it must be
	// stable across calls — the fingerprint is compared against persisted state, so an
	// answer that changed between two calls in one process would discard state at random.
	a := SiteFingerprint()
	b := SiteFingerprint()
	if a != b {
		t.Errorf("fingerprint is not stable within a process: %q vs %q", a, b)
	}
	if a != "" && len(a) != 16 {
		t.Errorf("a resolved fingerprint should be 16 hex chars, got %q", a)
	}
	t.Logf("this machine resolves to %q (empty is a valid answer: no gateway reachable)", a)
}
