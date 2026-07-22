package main

import (
	"strings"
	"testing"
)

// The recommendation has to be right for real machines, including the ones whose browsers
// refuse to describe them. Each case below is a machine an operator plausibly opens the
// dashboard on.
func TestRecommendModelPicksTierForRealMachines(t *testing.T) {
	cases := []struct {
		name    string
		profile HardwareProfile
		want    string
	}{
		{
			name:    "Apple Silicon laptop, Chrome (memory capped at 8)",
			profile: HardwareProfile{Cores: 10, MemoryGB: 8, Platform: "macOS", Arch: "arm64", GpuRenderer: "Apple M3 Pro"},
			want:    "forecast-accel",
		},
		{
			name:    "Apple Silicon laptop, Safari (reports no memory at all)",
			profile: HardwareProfile{Cores: 8, Platform: "macOS", Arch: "arm64", GpuRenderer: "Apple M2"},
			want:    "forecast-accel",
		},
		{
			name:    "NVIDIA workstation",
			profile: HardwareProfile{Cores: 16, MemoryGB: 8, Platform: "Windows", GpuRenderer: "NVIDIA GeForce RTX 4070"},
			want:    "forecast-accel",
		},
		{
			name:    "Big box with an AMD card — no dependable torch backend, so CPU",
			profile: HardwareProfile{Cores: 16, MemoryGB: 8, Platform: "Linux", GpuRenderer: "AMD Radeon RX 6800"},
			want:    "forecast-cpu",
		},
		{
			name:    "Office desktop, integrated graphics",
			profile: HardwareProfile{Cores: 8, MemoryGB: 8, Platform: "Windows", GpuRenderer: "Intel(R) UHD Graphics 620"},
			want:    "forecast-cpu",
		},
		{
			name:    "Ageing laptop",
			profile: HardwareProfile{Cores: 2, MemoryGB: 4, Platform: "Windows", GpuRenderer: "Intel(R) HD Graphics"},
			want:    "room-analyst",
		},
		{
			name:    "Very small / locked-down client",
			profile: HardwareProfile{Cores: 1, MemoryGB: 0.5},
			want:    "recommender-lite",
		},
		{
			name:    "Browser tells us nothing",
			profile: HardwareProfile{},
			want:    "room-analyst",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := recommendModel(tc.profile)
			if got.Recommended != tc.want {
				t.Errorf("recommended %q, want %q (rationale: %s)", got.Recommended, tc.want, got.Rationale)
			}
			// Whatever is recommended must actually be marked as fitting.
			for _, tier := range got.Tiers {
				if tier.Id == got.Recommended {
					if !tier.Fits {
						t.Errorf("recommended tier %q is marked as not fitting: %v", tier.Id, tier.Blockers)
					}
					if !tier.Recommended {
						t.Errorf("recommended tier %q is not flagged Recommended", tier.Id)
					}
				}
			}
			if got.Rationale == "" {
				t.Error("every recommendation must explain itself")
			}
		})
	}
}

// A tier that does not fit must say why, in terms the operator can act on.
func TestNonFittingTiersExplainThemselves(t *testing.T) {
	rec := recommendModel(HardwareProfile{Cores: 2, MemoryGB: 2, GpuRenderer: "Intel(R) UHD Graphics"})
	for _, tier := range rec.Tiers {
		if !tier.Fits && len(tier.Blockers) == 0 {
			t.Errorf("tier %q does not fit but gives no reason", tier.Id)
		}
	}
	// The accelerated tier must be blocked on the GPU, not silently dropped.
	for _, tier := range rec.Tiers {
		if tier.Id == "forecast-accel" {
			if tier.Fits {
				t.Fatal("forecast-accel must not fit a machine with integrated graphics")
			}
			found := false
			for _, b := range tier.Blockers {
				if len(b) > 0 && (b[0] == 'n') { // "needs ..."
					found = true
				}
			}
			if !found {
				t.Errorf("expected an actionable blocker, got %v", tier.Blockers)
			}
		}
	}
}

// Unmeasurable memory must be labelled as estimated, never presented as fact.
func TestUnknownMemoryIsLabelledHonestly(t *testing.T) {
	rec := recommendModel(HardwareProfile{Cores: 8, GpuRenderer: "Apple M2"})
	if rec.MemoryBasis != "estimated" {
		t.Errorf("memory basis should be 'estimated' when the browser reports none, got %q", rec.MemoryBasis)
	}
	if len(rec.Notes) == 0 {
		t.Error("an estimated profile must carry a note explaining the estimate")
	}

	reported := recommendModel(HardwareProfile{Cores: 8, MemoryGB: 8, GpuRenderer: "Apple M2"})
	if reported.MemoryBasis != "reported" {
		t.Errorf("memory basis should be 'reported' when the browser gives a value, got %q", reported.MemoryBasis)
	}
}

// Worker count drives real parallelism in the exported runtime, so it must be sane.
func TestWorkerCountIsSane(t *testing.T) {
	for _, tc := range []struct {
		cores, want int
	}{
		{0, 1}, {1, 1}, {2, 1}, {4, 3}, {8, 7}, {16, 8}, {64, 8},
	} {
		got := recommendModel(HardwareProfile{Cores: tc.cores}).Workers
		if got != tc.want {
			t.Errorf("%d cores -> %d workers, want %d", tc.cores, got, tc.want)
		}
	}
}

func TestClassifyGPU(t *testing.T) {
	for _, tc := range []struct {
		renderer    string
		wantKind    string
		wantCapable bool
	}{
		{"Apple M1 Max", "apple", true},
		{"ANGLE (NVIDIA GeForce RTX 3080 Direct3D11)", "nvidia", true},
		{"AMD Radeon Pro 5500M", "amd", false},
		{"Intel(R) Iris(TM) Plus Graphics", "integrated", false},
		{"Google SwiftShader", "integrated", false},
		{"", "unknown", false},
	} {
		got := classifyGPU(tc.renderer, false)
		if got.Kind != tc.wantKind || got.Capable != tc.wantCapable {
			t.Errorf("%q -> %+v, want kind=%s capable=%v", tc.renderer, got, tc.wantKind, tc.wantCapable)
		}
	}
}

// Browsers wrap the real GPU name in ANGLE boilerplate. The rationale is shown to a human,
// so it has to read like a machine description, not a driver string.
func TestCleanGPUName(t *testing.T) {
	for _, tc := range []struct{ renderer, want string }{
		{"ANGLE (Apple, ANGLE Metal Renderer: Apple M4, Unspecified Version)", "Apple M4"},
		{"ANGLE (Apple, ANGLE Metal Renderer: Apple M3 Pro, Unspecified Version)", "Apple M3 Pro"},
		{"Apple M1 Max", "Apple M1 Max"},
		{"ANGLE (NVIDIA, NVIDIA GeForce RTX 3080 Direct3D11 vs_5_0 ps_5_0, D3D11)", "NVIDIA GeForce RTX 3080"},
		{"Intel(R) Iris(TM) Plus Graphics", "Intel(R) Iris(TM) Plus Graphics"},
	} {
		if got := cleanGPUName(tc.renderer); got != tc.want {
			t.Errorf("cleanGPUName(%q) = %q, want %q", tc.renderer, got, tc.want)
		}
	}
	// Whatever the input, the name must stay short enough to sit inside a sentence.
	long := classifyGPU("ANGLE (SomeVendor, A Very Long Unrecognised Renderer String That Goes On And On And On, Version 9)", false)
	if len([]rune(long.Name)) > 50 {
		t.Errorf("unrecognised renderer name should be bounded, got %d chars: %q", len([]rune(long.Name)), long.Name)
	}
}

// The deviceMemory spec caps at 8, so exactly 8 is ambiguous — but a browser that reports
// 16 is giving a real figure and must not be told its number is meaningless.
func TestMemoryCapNoteOnlyForAmbiguousEight(t *testing.T) {
	hasCapNote := func(r ModelRecommendation) bool {
		for _, n := range r.Notes {
			if strings.Contains(n, "maximum the deviceMemory API") {
				return true
			}
		}
		return false
	}
	if !hasCapNote(recommendModel(HardwareProfile{Cores: 8, MemoryGB: 8})) {
		t.Error("a reported 8 GB is ambiguous and should be called out")
	}
	if hasCapNote(recommendModel(HardwareProfile{Cores: 10, MemoryGB: 16})) {
		t.Error("a reported 16 GB is a real figure and must not be flagged as capped")
	}
	if hasCapNote(recommendModel(HardwareProfile{Cores: 4, MemoryGB: 4})) {
		t.Error("a reported 4 GB is unambiguous and must not be flagged as capped")
	}
}

func TestTierLookupAndRank(t *testing.T) {
	if _, ok := tierByID("nope"); ok {
		t.Error("unknown tier must not validate")
	}
	if _, ok := tierByID("room-analyst"); !ok {
		t.Error("room-analyst should be a real tier")
	}
	if tierRank("recommender-lite") >= tierRank("forecast-accel") {
		t.Error("catalog must rank cheapest-first")
	}
}
