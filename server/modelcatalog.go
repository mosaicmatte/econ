package main

// Local model catalog — matching the downloadable intelligence to the machine that will run it.
//
// The export endpoint (modelexport.go) can ship anything from a few kilobytes of learned
// distributions to a PyTorch forecaster. Which of those a given operator SHOULD take is a
// hardware question: a stdlib recommender runs on a ten-year-old laptop, a GPU-accelerated
// scenario sweep does not. This file turns that into an honest recommendation instead of a
// menu the user has to guess at.
//
// The profile is measured on the client, because the client is the machine that matters —
// the server knows nothing about the browser's host. What the browser can report is
// genuinely limited, and this code is careful about that: navigator.deviceMemory is absent
// entirely on Safari and Firefox, and where it does exist the spec caps it at 8, so a
// reported "8" means "8 or more" while some browsers ignore the cap and report the real
// figure. Rather than pretend, an unmeasured value is labelled estimated, the estimate is
// conservative, an ambiguous 8 is called out, and the tier that depends on it says so.
//
//	POST /api/model/recommend  — hardware profile in, ranked tiers + a pick out
//
// The recommendation is not cosmetic: the chosen tier changes what the export actually
// contains and how much work the local runtime does (see econ_local.py and the tier
// parameter on /api/model/export).

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
)

// HardwareProfile is what the dashboard measures about the user's machine. Every field is
// optional: a browser that will not tell us something is handled, not assumed.
type HardwareProfile struct {
	Cores       int     `json:"cores"`       // navigator.hardwareConcurrency; 0 = unknown
	MemoryGB    float64 `json:"memoryGb"`    // navigator.deviceMemory; 0 = unknown
	Platform    string  `json:"platform"`    // "macOS", "Windows", "Linux", ...
	Arch        string  `json:"arch"`        // "arm64", "x86_64", ...
	GpuRenderer string  `json:"gpuRenderer"` // WebGL UNMASKED_RENDERER_WEBGL string
	HasWebGPU   bool    `json:"hasWebGpu"`
}

// gpuInfo is what we could work out about the accelerator, including the honest "we could
// not tell" case. Label is the raw renderer string (kept verbatim for diagnostics); Name is
// the readable model, because browsers wrap the real name in ANGLE boilerplate like
// "ANGLE (Apple, ANGLE Metal Renderer: Apple M4, Unspecified Version)".
type gpuInfo struct {
	Kind    string `json:"kind"`  // "apple" | "nvidia" | "amd" | "integrated" | "unknown"
	Accel   string `json:"accel"` // the torch backend this implies: "mps" | "cuda" | "" ...
	Label   string `json:"label"`
	Name    string `json:"name"`
	Capable bool   `json:"capable"` // can meaningfully accelerate a small model
}

// gpuNamePatterns pull the actual product name out of a vendor renderer string.
var gpuNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(Apple\s+M\d+(?:\s+(?:Pro|Max|Ultra))?)\b`),
	regexp.MustCompile(`(?i)\b(NVIDIA\s+(?:GeForce\s+)?(?:RTX|GTX)\s*\d+\s*(?:Ti|SUPER)?)\b`),
	regexp.MustCompile(`(?i)\b(Radeon\s+(?:RX\s+)?(?:Pro\s+)?[\w]+)\b`),
	regexp.MustCompile(`(?i)\b(Intel\(R\)\s+(?:UHD|Iris|HD)[\w\s\(\)]*?Graphics(?:\s+\d+)?)\b`),
}

// cleanGPUName reduces a renderer string to something worth showing a human.
func cleanGPUName(renderer string) string {
	for _, re := range gpuNamePatterns {
		if m := re.FindStringSubmatch(renderer); len(m) > 1 {
			return strings.Join(strings.Fields(m[1]), " ")
		}
	}
	// No known family matched: fall back to the raw string, trimmed of the ANGLE wrapper
	// and bounded so it cannot blow out a sentence.
	s := strings.TrimSpace(renderer)
	if i := strings.Index(s, "ANGLE ("); i == 0 {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "ANGLE ("), ")")
		if c := strings.Index(s, ","); c > 0 {
			s = s[:c]
		}
	}
	if len(s) > 48 {
		s = s[:48] + "…"
	}
	return s
}

// classifyGPU reads the WebGL renderer string. That string is the only GPU signal a
// browser exposes, and it is a vendor-formatted label rather than a spec, so this maps the
// families that actually change which torch backend is available and refuses to guess
// beyond that.
func classifyGPU(renderer string, hasWebGPU bool) gpuInfo {
	r := strings.ToLower(renderer)
	switch {
	case r == "":
		g := gpuInfo{Kind: "unknown", Label: "not reported", Name: "not reported"}
		if hasWebGPU {
			g.Label, g.Name = "not reported (WebGPU present)", "not reported"
		}
		return g
	case strings.Contains(r, "apple m") || strings.Contains(r, "apple gpu"):
		return gpuInfo{Kind: "apple", Accel: "mps", Label: renderer, Name: cleanGPUName(renderer), Capable: true}
	case strings.Contains(r, "nvidia") || strings.Contains(r, "geforce") ||
		strings.Contains(r, "rtx") || strings.Contains(r, "quadro"):
		return gpuInfo{Kind: "nvidia", Accel: "cuda", Label: renderer, Name: cleanGPUName(renderer), Capable: true}
	case strings.Contains(r, "radeon") || strings.Contains(r, "amd"):
		// ROCm/DirectML exist but are not a dependable default across platforms, so this
		// counts as present-but-not-assumed.
		return gpuInfo{Kind: "amd", Accel: "", Label: renderer, Name: cleanGPUName(renderer), Capable: false}
	case strings.Contains(r, "intel") || strings.Contains(r, "uhd") || strings.Contains(r, "iris"):
		return gpuInfo{Kind: "integrated", Accel: "", Label: renderer, Name: cleanGPUName(renderer), Capable: false}
	case strings.Contains(r, "swiftshader") || strings.Contains(r, "llvmpipe"):
		return gpuInfo{Kind: "integrated", Accel: "", Label: renderer + " (software)", Name: "software renderer", Capable: false}
	}
	return gpuInfo{Kind: "unknown", Label: renderer, Name: cleanGPUName(renderer)}
}

// ModelTier is one downloadable configuration: what it can do, and what it needs to do it.
type ModelTier struct {
	Id           string   `json:"id"`
	Name         string   `json:"name"`
	Summary      string   `json:"summary"`
	Capabilities []string `json:"capabilities"`
	Runtime      string   `json:"runtime"`
	MinCores     int      `json:"minCores"`
	MinMemoryGB  float64  `json:"minMemoryGb"`
	NeedsGPU     bool     `json:"needsGpu"`
	ApproxSizeMB int      `json:"approxSizeMb"`
}

// modelTiers is the catalog, cheapest first. The requirements are the real ones: the
// stdlib tiers need no packages at all, and the forecast tiers need a PyTorch install,
// which is where the multi-gigabyte footprint and the RAM floor come from.
var modelTiers = []ModelTier{
	{
		Id:      "recommender-lite",
		Name:    "Recommender (lite)",
		Summary: "The learned baselines plus a dependency-free scorer. Reproduces the dashboard's anomaly cards offline from a snapshot of readings.",
		Capabilities: []string{
			"σ-scores readings against the learned per-(zone, metric, hour) baselines",
			"Emits the same ranked alerts the server does, with the same learned-vs-standard basis",
			"Exit code doubles as an alert signal for cron/monitoring (0 ok, 1 warning, 2 critical)",
		},
		Runtime: "python3 (standard library only)", MinCores: 1, MinMemoryGB: 0.5, ApproxSizeMB: 1,
	},
	{
		Id:      "room-analyst",
		Name:    "Room analyst",
		Summary: "Adds every room's identified physical model, so the local copy predicts instead of only scoring — where each room is heading, when it breaches, and which rooms cannot hold setpoint at all.",
		Capabilities: []string{
			"Everything in the lite tier",
			"Integrates each room's learned thermal and CO₂ balances forward in closed form",
			"Multi-horizon sweep (15/30/60/120 min) across every room, ranked by predicted risk",
			"Flags rooms whose learned cooling authority cannot hold their setpoint",
			"Batch-replays a whole history file instead of a single snapshot",
			"Parallelises across cores with multiprocessing",
		},
		Runtime: "python3 (standard library only)", MinCores: 2, MinMemoryGB: 2, ApproxSizeMB: 2,
	},
	{
		Id:      "forecast-cpu",
		Name:    "Forecast (CPU)",
		Summary: "Adds building-load forecasting: the supervised LSTM trained on this building, plus Google TimesFM — a pretrained foundation model that forecasts the load zero-shot, with no training at all. Runs on the CPU.",
		Capabilities: []string{
			"Everything in the room analyst tier",
			"Google TimesFM (231M) forecasts the load zero-shot — works with no trained artifacts",
			"Quantile forecasts, so a pre-cool decision can be made on peak RISK, not a bare mean",
			"Runs the supervised LSTM too, once it has been trained on this building",
			"Couples the load forecast to the room models to pick pre-cool windows offline",
		},
		Runtime: "python3 + torch + transformers (CPU)", MinCores: 4, MinMemoryGB: 8, ApproxSizeMB: 2400,
	},
	{
		Id:      "forecast-accel",
		Name:    "Forecast (accelerated)",
		Summary: "The full stack with GPU acceleration — Apple Silicon MPS or CUDA. TimesFM inference moves onto the accelerator and the horizon sweep runs as a batched scenario ensemble rather than one trajectory.",
		Capabilities: []string{
			"Everything in the CPU forecast tier",
			"TimesFM and LSTM inference on MPS (Apple Silicon) or CUDA",
			"Headroom for the larger TimesFM 2.0 (500M) checkpoint",
			"Batched Monte-Carlo occupancy scenarios per room, not a single trajectory",
			"Dense horizon sweep for whole-building pre-cool scheduling",
		},
		Runtime: "python3 + torch + transformers (MPS/CUDA)", MinCores: 8, MinMemoryGB: 16, NeedsGPU: true, ApproxSizeMB: 4200,
	},
}

// TierFit is one catalog entry judged against a specific machine.
type TierFit struct {
	ModelTier
	Fits        bool     `json:"fits"`
	Recommended bool     `json:"recommended"`
	Blockers    []string `json:"blockers"`
}

// ModelRecommendation is the POST /api/model/recommend response.
type ModelRecommendation struct {
	Profile     HardwareProfile `json:"profile"`
	GPU         gpuInfo         `json:"gpu"`
	EffectiveGB float64         `json:"effectiveMemoryGb"`
	MemoryBasis string          `json:"memoryBasis"` // "reported" | "estimated"
	CoresBasis  string          `json:"coresBasis"`
	Recommended string          `json:"recommended"` // tier id
	Workers     int             `json:"workers"`     // parallelism the local runtime should use
	Tiers       []TierFit       `json:"tiers"`
	Notes       []string        `json:"notes"`
	Rationale   string          `json:"rationale"`
}

// recommendModel is the whole decision, kept pure so it is directly testable.
func recommendModel(p HardwareProfile) ModelRecommendation {
	rec := ModelRecommendation{Profile: p}
	rec.GPU = classifyGPU(p.GpuRenderer, p.HasWebGPU)

	// Cores. hardwareConcurrency is reliable where it exists; some privacy modes clamp it.
	cores := p.Cores
	rec.CoresBasis = "reported"
	if cores <= 0 {
		cores = 2
		rec.CoresBasis = "assumed"
		rec.Notes = append(rec.Notes,
			"The browser did not report a core count, so 2 cores were assumed. Pick a higher tier manually if the machine is larger.")
	}

	// Memory. This is the field browsers are worst at, so it gets the most care.
	rec.EffectiveGB = p.MemoryGB
	rec.MemoryBasis = "reported"
	switch {
	case p.MemoryGB <= 0:
		// Nothing reported (Safari, Firefox). Estimate conservatively from core count
		// rather than optimistically — under-recommending costs the user a smaller
		// download, over-recommending costs them a tier that will not run.
		rec.EffectiveGB = math.Min(float64(cores)*1.5, 8)
		rec.MemoryBasis = "estimated"
		rec.Notes = append(rec.Notes,
			fmt.Sprintf("This browser does not expose system memory, so ~%.0f GB was estimated from the %d reported cores. The estimate is deliberately conservative.", rec.EffectiveGB, cores))
	case p.MemoryGB == 8:
		// The deviceMemory spec caps the reported value at 8, so exactly "8" is ambiguous:
		// it means "8 or more" and nothing finer. Larger values (some browsers exceed the
		// spec) are real figures and are reported as such.
		rec.Notes = append(rec.Notes,
			"This browser reports 8 GB, which is the maximum the deviceMemory API will admit to — the machine may well have more. Tiers above the CPU forecaster are therefore judged on cores and GPU rather than on this number.")
	}

	// Fit each tier. GPU-requiring tiers additionally need an accelerator we can actually
	// name a torch backend for — an AMD card in a browser string is not a promise of ROCm.
	best := ""
	for _, t := range modelTiers {
		fit := TierFit{ModelTier: t, Fits: true}
		if cores < t.MinCores {
			fit.Fits = false
			fit.Blockers = append(fit.Blockers,
				fmt.Sprintf("needs %d cores, this machine reports %d", t.MinCores, cores))
		}
		// The 8 GB browser cap means the top tier's 16 GB floor is unverifiable; judge it
		// on the accelerator instead of failing every machine on an artefact of the API.
		if t.MinMemoryGB <= 8 && rec.EffectiveGB < t.MinMemoryGB {
			fit.Fits = false
			fit.Blockers = append(fit.Blockers,
				fmt.Sprintf("needs ~%.0f GB of memory, this machine %s %.0f GB",
					t.MinMemoryGB, map[bool]string{true: "reports", false: "is estimated at"}[rec.MemoryBasis == "reported"], rec.EffectiveGB))
		}
		if t.NeedsGPU && !rec.GPU.Capable {
			fit.Fits = false
			fit.Blockers = append(fit.Blockers,
				"needs an Apple Silicon or NVIDIA GPU with a supported torch backend; detected: "+rec.GPU.Name)
		}
		if fit.Fits {
			best = t.Id
		}
		rec.Tiers = append(rec.Tiers, fit)
	}
	if best == "" {
		best = modelTiers[0].Id // the lite tier has no meaningful floor
	}
	for i := range rec.Tiers {
		if rec.Tiers[i].Id == best {
			rec.Tiers[i].Recommended = true
		}
	}
	rec.Recommended = best

	// Parallelism for the local runtime: leave a core for the OS, and don't spawn workers
	// a stdlib multiprocessing pool will only spend its time scheduling.
	rec.Workers = cores - 1
	if rec.Workers < 1 {
		rec.Workers = 1
	}
	if rec.Workers > 8 {
		rec.Workers = 8
	}

	rec.Rationale = rationaleFor(best, cores, rec)
	return rec
}

func rationaleFor(tier string, cores int, rec ModelRecommendation) string {
	switch tier {
	case "forecast-accel":
		return fmt.Sprintf("%d cores alongside %s (torch %s backend) — enough to run the LSTM as a batched scenario ensemble, so the local copy does the heaviest version of the analysis.",
			cores, rec.GPU.Name, rec.GPU.Accel)
	case "forecast-cpu":
		reason := "no GPU with a supported torch backend was detected, so the forecaster runs on CPU"
		if rec.GPU.Capable {
			reason = "the accelerated tier's core count was not met, so the forecaster runs on CPU"
		}
		return fmt.Sprintf("%d cores and ~%.0f GB of memory carry the LSTM comfortably; %s.",
			cores, rec.EffectiveGB, reason)
	case "room-analyst":
		return fmt.Sprintf("%d cores and ~%.0f GB is comfortably enough for the full room-prediction analysis, and it needs no packages at all — but not enough headroom to ask this machine to install and run PyTorch.",
			cores, rec.EffectiveGB)
	}
	return "This machine is best served by the dependency-free scorer, which runs anywhere Python 3 does and needs no packages."
}

// tierById looks a tier up, reporting whether it exists — used by the export endpoint to
// validate the ?tier= parameter rather than trusting it.
func tierByID(id string) (ModelTier, bool) {
	for _, t := range modelTiers {
		if t.Id == id {
			return t, true
		}
	}
	return ModelTier{}, false
}

// tierRank is the catalog position, so the export can ask "is this tier at least X".
func tierRank(id string) int {
	for i, t := range modelTiers {
		if t.Id == id {
			return i
		}
	}
	return 0
}

func modelRecommendHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		var p HardwareProfile
		if r.Body != nil {
			// A malformed or absent body is not an error: it just means we know nothing
			// about the machine, and recommendModel already handles that honestly.
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&p)
		}
		json.NewEncoder(w).Encode(recommendModel(p))
	}
}
