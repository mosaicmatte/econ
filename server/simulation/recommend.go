package simulation

// Recommendations — the intelligence the dashboard's "AI Operations" panel shows.
//
// This is what replaces the old hardcoded threshold cards. Each recommendation is scored
// against the learned baseline model (baselines.go): a reading is flagged because it sits
// many σ from what that specific zone normally does at this hour, and the message says so
// ("6.2σ above this room's 14:00 normal of 620±90 ppm"), not because it crossed a number
// someone hardcoded. A recognized standard (the ASHRAE CO2 guideline) is the labelled
// cold-start floor while a zone's baseline is still learning, so the panel is never blank
// AND never presents an immature baseline as if it were established.
//
// Every recommendation maps to a real remediation the engine already actuates over the
// websocket (purge, cool, precool), so acting on one changes the building, not a label.

import (
	"fmt"
	"sort"
	"time"
)

// ZoneReading is one zone's current state, gathered by the engine under its own lock and
// handed to the (independently-locked) baseline model for scoring — so the recommendation
// path never holds both locks at once.
type ZoneReading struct {
	Zone      string
	Label     string
	Type      string
	Temp      float64
	Setpoint  float64
	Occupancy int
	Co2       float64 // ppm; only meaningful when Co2Live
	Co2Live   bool    // a fresh NDIR sensor is measuring this zone right now
}

// Recommendation is one ranked, actionable insight. The structured fields (Baseline,
// Sigma, Deviation, Samples, Basis) let the UI render the learned context faithfully
// instead of re-deriving it, and Action names a real websocket remediation.
type Recommendation struct {
	Id        string  `json:"id"`
	Zone      string  `json:"zone"`
	Label     string  `json:"label"`
	Metric    string  `json:"metric"`
	Severity  string  `json:"severity"` // "critical" | "warning" | "info"
	Basis     string  `json:"basis"`    // "learned" | "standard"
	Title     string  `json:"title"`
	Message   string  `json:"message"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Baseline  float64 `json:"baseline"`  // learned mean (0 when basis="standard")
	Sigma     float64 `json:"sigma"`     // learned σ (0 when basis="standard")
	Deviation float64 `json:"deviation"` // signed z-score (0 when basis="standard")
	Samples   int     `json:"samples"`   // baseline maturity behind this call
	Hour      int     `json:"hour"`      // which learned bucket answered (24 = pooled)
	Action    string  `json:"action"`    // websocket action, "" = advisory only
	Score     float64 `json:"-"`         // internal ranking key
}

// RecommendationReport is the /api/recommendations payload: the ranked list plus an honest
// readout of the model's own maturity, so a short list reads unambiguously.
type RecommendationReport struct {
	Recommendations []Recommendation `json:"recommendations"`
	Model           struct {
		Established      int      `json:"established"` // trusted (zone,metric,hour) buckets
		Learning         int      `json:"learning"`    // buckets still warming up
		MatureAfter      int      `json:"matureAfter"` // samples a bucket needs
		SampleCadenceSec int      `json:"sampleCadenceSec"`
		Metrics          []string `json:"metrics"`
	} `json:"model"`
}

// hourLabel renders which learned bucket answered: a clock hour ("14:00") or the
// all-hours pooled fallback used before any single hour has matured.
func hourLabel(h int) string {
	if h == pooledHour {
		return "all-hours"
	}
	return fmt.Sprintf("%02d:00", h)
}

func severityFor(z float64) string {
	if z >= 5.0 {
		return "critical"
	}
	return "warning"
}

// Recommend scores the current readings against the learned model and returns a ranked
// report. Concurrency-safe: it takes the baseline lock for the scoring pass only.
func (b *Baselines) Recommend(readings []ZoneReading, loadMw float64, now time.Time, topN int) RecommendationReport {
	recs := []Recommendation{} // non-nil so an empty result marshals as [] rather than null

	b.mu.Lock()
	for _, zr := range readings {
		// Temperature: purely learned (no universal fixed limit). Flag a zone many σ
		// hotter than its own hourly normal AND actually above its setpoint — a hot room
		// that is at or below setpoint is being cooled correctly, not drifting.
		if spec, ok := metricSpecs["temp"]; ok && zr.Setpoint > 0 && zr.Temp > zr.Setpoint {
			if sc := b.score(zr.Zone, "temp", zr.Temp, now, spec); sc.mature && sc.z >= spec.zAlert {
				recs = append(recs, Recommendation{
					Id: "temp:" + zr.Zone, Zone: zr.Zone, Label: zr.Label, Metric: "temp",
					Severity: severityFor(sc.z), Basis: "learned",
					Title: "Zone Running Hot vs Its Learned Normal",
					Message: fmt.Sprintf("%s is at %.1f°C — %.1fσ above its own typical %s temperature of %.1f±%.1f°C (learned from %d samples), setpoint %.1f°C. Likely a starved VAV or a load spike before it crosses any fixed deadband; flood cooling or release its damper.",
						zr.Label, zr.Temp, sc.z, hourLabel(sc.hour), sc.mean, sc.std, sc.count, zr.Setpoint),
					Value: zr.Temp, Unit: "°C", Baseline: sc.mean, Sigma: sc.std, Deviation: sc.z,
					Samples: sc.count, Hour: sc.hour, Action: spec.action, Score: sc.z,
				})
			}
		}

		// CO2: learned when the sensor's baseline is established, else the ASHRAE standard
		// floor — clearly labelled which basis fired.
		if zr.Co2Live {
			spec := metricSpecs["co2"]
			sc := b.score(zr.Zone, "co2", zr.Co2, now, spec)
			switch {
			case sc.mature && sc.z >= spec.zAlert:
				recs = append(recs, Recommendation{
					Id: "co2:" + zr.Zone, Zone: zr.Zone, Label: zr.Label, Metric: "co2",
					Severity: severityFor(sc.z), Basis: "learned",
					Title: "CO₂ Anomaly vs Learned Normal",
					Message: fmt.Sprintf("%s reads %.0f ppm from its NDIR sensor — %.1fσ above its usual %s level of %.0f±%.0f ppm (learned from %d samples). Ventilation isn't matching occupancy; purge the zone.",
						zr.Label, zr.Co2, sc.z, hourLabel(sc.hour), sc.mean, sc.std, sc.count),
					Value: zr.Co2, Unit: "ppm", Baseline: sc.mean, Sigma: sc.std, Deviation: sc.z,
					Samples: sc.count, Hour: sc.hour, Action: spec.action, Score: sc.z,
				})
			case zr.Co2 > spec.standardHi:
				recs = append(recs, Recommendation{
					Id: "co2:" + zr.Zone, Zone: zr.Zone, Label: zr.Label, Metric: "co2",
					Severity: "warning", Basis: "standard",
					Title: "CO₂ Above ASHRAE Guideline",
					Message: fmt.Sprintf("%s reads %.0f ppm (comfort guideline ≤ %.0f ppm). This zone's baseline is still learning (%d/%d samples), so this is the recognized-standard check, not a learned anomaly. Purge the zone.",
						zr.Label, zr.Co2, spec.standardHi, sc.count, baselineMature),
					Value: zr.Co2, Unit: "ppm", Samples: sc.count, Action: spec.action,
					Score: 2.0 + (zr.Co2/spec.standardHi - 1.0),
				})
			}
		}
	}

	// Whole-building load: the automation-grade signal, surfaced as an advisory with the
	// pre-cool action when the live load is running far above the building's own learned
	// normal for this hour.
	if spec, ok := metricSpecs["buildingLoadMw"]; ok && loadMw > 0 {
		if sc := b.score("GLOBAL", "buildingLoadMw", loadMw, now, spec); sc.mature && sc.z >= spec.zAlert {
			sev := "info"
			if sc.z >= 3.0 {
				sev = "warning"
			}
			recs = append(recs, Recommendation{
				Id: "load:GLOBAL", Zone: "GLOBAL", Label: "Whole building", Metric: "buildingLoadMw",
				Severity: sev, Basis: "learned",
				Title: "Building Load High vs Learned Normal",
				Message: fmt.Sprintf("Whole-building load is %.2f MW — %.1fσ above its learned %s normal of %.2f±%.2f MW. Pre-cooling now charges the thermal mass so chillers can shed load off the coming peak.",
					loadMw, sc.z, hourLabel(sc.hour), sc.mean, sc.std),
				Value: loadMw, Unit: "MW", Baseline: sc.mean, Sigma: sc.std, Deviation: sc.z,
				Samples: sc.count, Hour: sc.hour, Action: spec.action, Score: sc.z * 0.9,
			})
		}
	}
	b.mu.Unlock()

	// Rank most-severe first; stable so equal scores keep input (zone) order.
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Score > recs[j].Score })
	if topN > 0 && len(recs) > topN {
		recs = recs[:topN]
	}

	est, learn := b.Coverage()
	var report RecommendationReport
	report.Recommendations = recs
	report.Model.Established = est
	report.Model.Learning = learn
	report.Model.MatureAfter = baselineMature
	report.Model.SampleCadenceSec = baselineSampleSecs
	report.Model.Metrics = []string{"temp", "co2", "buildingLoadMw", "plugKw"}
	return report
}
