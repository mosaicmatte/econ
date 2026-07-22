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

	// Predictive fields, set only by the learned room-dynamics pass (dynamics.go). Kind
	// distinguishes what sort of judgement this is, so the UI can badge a forecast
	// differently from a present-tense anomaly instead of implying they are the same.
	Kind        string  `json:"kind"`        // "anomaly" | "prediction" | "capability"
	EtaSec      float64 `json:"etaSec"`      // seconds until the predicted breach (0 = n/a)
	Predicted   float64 `json:"predicted"`   // value at the prediction horizon
	Equilibrium float64 `json:"equilibrium"` // value the room settles at if nothing changes
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
		// Room-dynamics maturity (dynamics.go): how many rooms the twin has actually
		// identified a physical model for, vs. how many it is still learning. Reported
		// separately from the baselines because they mature on different evidence — a
		// room must MOVE to reveal its dynamics, not merely be observed.
		RoomsIdentified int `json:"roomsIdentified"`
		RoomsLearning   int `json:"roomsLearning"`
		HorizonMin      int `json:"horizonMin"`
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

	// Everything this pass produces is a present-tense anomaly; the forward-looking
	// judgements come from the room-dynamics pass and label themselves.
	for i := range recs {
		recs[i].Kind = "anomaly"
	}

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
	report.Model.Metrics = []string{"temp", "co2", "buildingLoadMw", "plugKw", "occupancy"}
	report.Model.HorizonMin = int(predictHorizonSec / 60)
	return report
}

// --- predictive pass (learned room dynamics) ---------------------------------

const (
	// predictHorizonSec is how far ahead the identified room models are integrated. Half
	// an hour is the operationally useful window: long enough that acting now actually
	// prevents the breach, short enough that "hold the current drivers" is a fair
	// assumption for a first-order model.
	predictHorizonSec = 1800.0
	// predictMinEtaSec suppresses predictions so imminent they are not actionable — at
	// that point the anomaly pass is already reporting the condition itself.
	predictMinEtaSec = 120.0
	// predictCriticalEtaSec is when a forecast breach stops being something to schedule
	// and becomes something to do now.
	predictCriticalEtaSec = 600.0
	// capacityMarginC is how far above setpoint a room's FULL-FLOW equilibrium must sit
	// before the twin will say the room cannot be held. A small margin absorbs
	// identification error rather than crying wolf on every borderline room.
	capacityMarginC = 0.8
)

// etaLabel renders a predicted time-to-breach the way an operator reads it.
func etaLabel(sec float64) string {
	if sec >= 5400 {
		return fmt.Sprintf("%.1f h", sec/3600)
	}
	return fmt.Sprintf("%.0f min", sec/60)
}

// PredictiveRecommendations runs every identified room forward and reports the breaches
// that are coming, plus the rooms whose learned cooling authority means they cannot hold
// their own setpoint at all.
//
// This is the part that is genuinely not rule-based: nothing here compares a reading to a
// number. Each finding is the closed-form solution of that specific room's identified
// energy or mass balance — its own time constant, its own cooling authority, its own
// air-change rate — evaluated at the drivers it has right now. Two rooms at exactly the
// same temperature and occupancy will produce different findings, because they are
// different rooms and the model knows it.
func (d *Dynamics) PredictiveRecommendations(conds []RoomCondition, now time.Time) []Recommendation {
	out := []Recommendation{}

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, c := range conds {
		zd := d.rooms[c.Zone]
		if zd == nil {
			continue
		}
		label := c.Label
		if label == "" {
			label = c.Zone
		}

		// --- thermal: where is this room heading, and can it be held at all? ---
		if zd.thermalUsable() && c.Setpoint > 0 {
			limit := c.Setpoint + 1.0 // the comfort ceiling the prediction is measured against
			look := d.thermalOutlookLocked(c, limit, predictHorizonSec)

			// A room that cannot hold setpoint even at full flow is a capability
			// shortfall, not a transient — the strongest thing the identified model can
			// say, and unreachable from any threshold on a reading.
			full := c
			full.FlowRatio = 1.0
			if atFull := d.thermalOutlookLocked(full, limit, predictHorizonSec); atFull.Ok &&
				atFull.Equilibrium > c.Setpoint+capacityMarginC {
				out = append(out, Recommendation{
					Id: "capacity:" + c.Zone, Zone: c.Zone, Label: label, Metric: "coolingAuthority",
					Severity: severityForCapacity(atFull.Equilibrium - c.Setpoint),
					Basis:    "learned", Kind: "capability",
					Title: "Room Cannot Hold Setpoint At Full Cooling",
					Message: fmt.Sprintf("%s settles at %.1f°C even with its VAV wide open — %.1f°C above its %.1f°C setpoint. Identified from %d samples of this room's own response (time constant %.0f min). This is a capacity or delivery fault (undersized VAV, fouled coil, stuck damper, or a load this room was never designed for), not a control problem — dispatch maintenance rather than re-tuning the setpoint.",
						label, atFull.Equilibrium, atFull.Equilibrium-c.Setpoint, c.Setpoint,
						zd.Thermal.N, atFull.TauMin),
					Value: c.Temp, Unit: "°C", Equilibrium: atFull.Equilibrium,
					Samples: zd.Thermal.N, Action: "cool",
					Score: 5.0 + (atFull.Equilibrium - c.Setpoint),
				})
			} else if look.Ok && look.SecsToLimit > predictMinEtaSec && look.SecsToLimit < predictHorizonSec {
				// A breach is coming under the drivers the room has right now.
				sev := "warning"
				if look.SecsToLimit < predictCriticalEtaSec {
					sev = "critical"
				}
				// A far-extrapolated settling point is reported as a direction of travel,
				// not a figure: the crossing time is the part of the curve the model
				// actually identified, so that is what the operator is given to act on.
				heading := fmt.Sprintf("heading for %.1f°C", look.Equilibrium)
				if look.Extrapolated {
					heading = fmt.Sprintf("climbing well past %.0f°C (its identified response is being extrapolated beyond anything this room has been observed doing, so use the crossing time, not the settling point)", credibleMaxC)
				}
				out = append(out, Recommendation{
					Id: "predict-temp:" + c.Zone, Zone: c.Zone, Label: label, Metric: "temp",
					Severity: sev, Basis: "learned", Kind: "prediction",
					Title: "Zone Predicted To Breach Comfort",
					Message: fmt.Sprintf("%s is %.1f°C now and %s — it crosses %.1f°C in about %s. Predicted from this room's own identified response (time constant %.0f min, learned from %d samples) at its current %.0f%% airflow and %d occupants. Increasing flow now costs less than recovering the room after it drifts.",
						label, c.Temp, heading, limit, etaLabel(look.SecsToLimit),
						look.TauMin, zd.Thermal.N, c.FlowRatio*100, c.Occupancy),
					Value: c.Temp, Unit: "°C", Predicted: look.Predicted,
					Equilibrium: look.Equilibrium, EtaSec: look.SecsToLimit,
					Samples: zd.Thermal.N, Action: "cool",
					Score: 4.0 + 4.0*(1.0-look.SecsToLimit/predictHorizonSec),
				})
			}
		}

		// --- ventilation: does this room's measured air-change rate cope with its load? ---
		if zd.co2Usable() && c.Co2Live {
			limit := metricSpecs["co2"].standardHi
			look := d.co2OutlookLocked(c, limit, predictHorizonSec)
			if look.Ok && look.Equilibrium > limit && c.Co2 < limit {
				eta := look.SecsToLimit
				if eta > predictMinEtaSec && eta < predictHorizonSec {
					sev := "warning"
					if eta < predictCriticalEtaSec {
						sev = "critical"
					}
					heading := fmt.Sprintf("heading for %.0f ppm", look.Equilibrium)
					if look.Extrapolated {
						heading = fmt.Sprintf("climbing well past %.0f ppm (beyond the range this room's balance was identified over — act on the crossing time)", co2CredibleMax)
					}
					out = append(out, Recommendation{
						Id: "predict-co2:" + c.Zone, Zone: c.Zone, Label: label, Metric: "co2",
						Severity: sev, Basis: "learned", Kind: "prediction",
						Title: "Ventilation Will Not Keep Up With Occupancy",
						Message: fmt.Sprintf("%s reads %.0f ppm and is %s at its current %d occupants — past the %.0f ppm guideline in about %s. This room's measured air-change rate is %.1f ACH (identified from %d samples); that is not enough ventilation for this many people. Purge now, before it becomes a complaint.",
							label, c.Co2, heading, c.Occupancy, limit,
							etaLabel(eta), look.AchPerHour, zd.Co2.N),
						Value: c.Co2, Unit: "ppm", Predicted: look.Predicted,
						Equilibrium: look.Equilibrium, EtaSec: eta,
						Samples: zd.Co2.N, Action: "purge",
						Score: 4.0 + 4.0*(1.0-eta/predictHorizonSec),
					})
				}
			}
		}
	}
	return out
}

func severityForCapacity(excessC float64) string {
	if excessC >= 2.0 {
		return "critical"
	}
	return "warning"
}

// mergeRecommendations folds the predictive pass into the anomaly report, ranks the
// combined list, and applies topN.
//
// A predictive finding for a (zone, metric) that is ALREADY anomalous is dropped: the
// present-tense card is both more certain and more urgent, and showing "this room is hot"
// beside "this room will be hot" reads as two problems when it is one.
func mergeRecommendations(report RecommendationReport, extra []Recommendation, topN int) RecommendationReport {
	seen := make(map[string]bool, len(report.Recommendations))
	for _, r := range report.Recommendations {
		seen[r.Zone+"\x1f"+r.Metric] = true
	}
	recs := report.Recommendations
	for _, r := range extra {
		if seen[r.Zone+"\x1f"+r.Metric] {
			continue
		}
		recs = append(recs, r)
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Score > recs[j].Score })
	if topN > 0 && len(recs) > topN {
		recs = recs[:topN]
	}
	report.Recommendations = recs
	return report
}
