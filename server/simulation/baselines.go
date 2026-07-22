package simulation

// Learned operating baselines — the model that replaces hardcoded thresholds.
//
// Every "AI recommendation" the twin used to make was a fixed rule: co2 > 1000,
// temp > setpoint + deadband, forecast >= 2.0 MW. A fixed number is wrong for every
// building but the one it was tuned on, and it has no idea what "normal" is for a given
// room at a given time of day. This model learns that instead.
//
// For each (zone, metric, hour-of-day) it keeps an online estimate of the signal's mean
// and spread — an incremental exponentially-weighted mean/variance, so it needs no stored
// history, adapts as the building changes, and is O(1) per observation. An anomaly is then
// a reading that sits many σ away from what THIS zone normally does at THIS hour, not a
// reading past a number someone typed in. The same learned load distribution decides when
// pre-cooling is worth it (precool.go), so one model drives both the recommendations and
// the automation.
//
// Honesty discipline (the same one the sensors follow): a bucket that has not seen enough
// observations is "still learning" and is never used to raise an alarm — the caller falls
// back to a recognized standard (e.g. the ASHRAE 1000 ppm CO2 guideline) and says so. The
// model reports its own maturity so the UI can show "learned 14:00 normal" vs "warming up".

import (
	"encoding/json"
	"math"
	"sync"
	"time"
)

const (
	// baselineAlphaFloor bounds how much any single observation can move a mature
	// bucket: early on the update behaves like a plain running mean (1/n) for fast
	// convergence, then floors at this decay so the baseline keeps adapting to slow
	// building changes without chasing every wiggle. ~1/0.04 = an effective memory of
	// ~25 observations per bucket.
	baselineAlphaFloor = 0.04
	// baselineMature is how many observations a bucket needs before it is trusted to
	// score anomalies. Below this the bucket is "learning" and the caller uses a
	// standard/floor instead of the immature mean.
	baselineMature = 20
	// pooledHour is the synthetic hour key for the all-hours ("pooled") bucket, learned
	// alongside the 24 clock-hour buckets so a metric can be scored before any single
	// hour has matured — the demo-friendly cold path that is still a learned normal.
	pooledHour = 24
	// baselineSampleSecs is the cadence the engine folds observations in at. Slower than
	// the 1 Hz DB persist on purpose: at one sample per ~20 s an hour-bucket gathers a
	// few hundred samples a day, so its EWMA memory spans days (a real diurnal normal)
	// rather than the last few minutes.
	baselineSampleSecs = 20
)

// metricSpec is the per-metric knowledge the model can't learn: the floor on σ (so a
// dead-flat series doesn't yield an infinite z-score), how many σ warrant a callout, the
// direction that is actually a problem, an optional recognized standard for the cold
// path, and the real remediation action the dashboard already knows how to fire.
type metricSpec struct {
	minSigma   float64 // σ floor in the metric's own units
	zAlert     float64 // |deviation| in σ that raises a recommendation
	hiIsBad    bool    // an unusually HIGH reading warrants action (co2, temp, load)
	standardHi float64 // recognized fixed limit for the cold path; 0 = none
	action     string  // websocket action that remediates it ("" = advisory only)
	unit       string
	label      string
}

var metricSpecs = map[string]metricSpec{
	// CO2: NDIR only (the engine feeds it only fresh sensor readings). ASHRAE/TCVN put
	// the comfort/ventilation guideline at ~1000 ppm — a real standard, so it is the
	// honest floor while the per-zone baseline is still learning.
	"co2": {minSigma: 40, zAlert: 3.0, hiIsBad: true, standardHi: 1000, action: "purge", unit: "ppm", label: "CO₂"},
	// Room temperature: a zone running many σ hotter than its own hourly normal is a
	// developing thermal problem (starved VAV, load spike) before it crosses any fixed
	// deadband. No universal fixed limit — this is a purely learned signal.
	"temp": {minSigma: 0.4, zAlert: 3.5, hiIsBad: true, action: "cool", unit: "°C", label: "temperature"},
	// Whole-building electrical load: the automation signal. A forecast peak far above
	// the building's own learned load for the coming hour is what makes pre-cooling pay.
	"buildingLoadMw": {minSigma: 0.05, zAlert: 1.5, hiIsBad: true, action: "precool", unit: "MW", label: "building load"},
	// Live plug draw (kW): a sudden climb above the after-hours norm is phantom load the
	// sweep should be catching.
	"plugKw": {minSigma: 0.3, zAlert: 3.0, hiIsBad: true, action: "", unit: "kW", label: "plug load"},
}

// MetricSpecPublic is the JSON-facing view of a metricSpec — everything a downloaded copy
// of the model needs to reproduce the engine's scoring exactly (recommender.py).
type MetricSpecPublic struct {
	MinSigma   float64 `json:"minSigma"`
	ZAlert     float64 `json:"zAlert"`
	HiIsBad    bool    `json:"hiIsBad"`
	StandardHi float64 `json:"standardHi"`
	Action     string  `json:"action"`
	Unit       string  `json:"unit"`
	Label      string  `json:"label"`
}

// ModelSpec is the portable description of the learned model: its parameters plus the
// per-metric thresholds, exported alongside the learned distributions so an offline
// recommender scores identically to the live engine.
type ModelSpec struct {
	MatureAfter      int                         `json:"matureAfter"`
	PooledHour       int                         `json:"pooledHour"`
	SampleCadenceSec int                         `json:"sampleCadenceSec"`
	AlphaFloor       float64                     `json:"alphaFloor"`
	Metrics          map[string]MetricSpecPublic `json:"metrics"`
}

// BaselineModelSpec renders the current model parameters + metric specs for export.
func BaselineModelSpec() ModelSpec {
	m := make(map[string]MetricSpecPublic, len(metricSpecs))
	for k, s := range metricSpecs {
		m[k] = MetricSpecPublic{
			MinSigma: s.minSigma, ZAlert: s.zAlert, HiIsBad: s.hiIsBad,
			StandardHi: s.standardHi, Action: s.action, Unit: s.unit, Label: s.label,
		}
	}
	return ModelSpec{
		MatureAfter:      baselineMature,
		PooledHour:       pooledHour,
		SampleCadenceSec: baselineSampleSecs,
		AlphaFloor:       baselineAlphaFloor,
		Metrics:          m,
	}
}

// baselineStat is one learned distribution: an EWMA mean and variance plus the number of
// observations folded in (its maturity). Exported fields so it round-trips through JSON
// for persistence without a shadow struct.
type baselineStat struct {
	Mean  float64 `json:"m"`
	Var   float64 `json:"v"`
	Count int     `json:"n"`
}

// observe folds one reading into the stat. Standard incremental weighted mean/variance
// (Finch 2009): the weight is 1/n early for fast convergence, floored at alphaFloor so a
// mature bucket keeps adapting instead of freezing.
func (s *baselineStat) observe(x, alphaFloor float64) {
	s.Count++
	if s.Count == 1 {
		s.Mean = x
		s.Var = 0
		return
	}
	a := 1.0 / float64(s.Count)
	if a < alphaFloor {
		a = alphaFloor
	}
	d := x - s.Mean
	s.Mean += a * d
	s.Var = (1 - a) * (s.Var + a*d*d)
}

func (s *baselineStat) std() float64 { return math.Sqrt(math.Max(0, s.Var)) }

// Baselines is the concurrency-safe collection of learned distributions. It owns its own
// lock and never reaches back into the engine, so the engine can fold observations in
// while holding e.mu (e.mu → baselines.mu) and the HTTP/scoring path can read it after
// releasing e.mu — one consistent lock order, no cycle.
type Baselines struct {
	mu    sync.Mutex
	stats map[string]map[int]*baselineStat // metricKey(zone,metric) -> hour(0..24) -> stat
}

func NewBaselines() *Baselines {
	return &Baselines{stats: make(map[string]map[int]*baselineStat)}
}

func baselineKey(zone, metric string) string { return zone + "\x1f" + metric }

// Observe folds one reading into both the current clock-hour bucket and the pooled
// bucket. Called from the engine tick (lock held there is fine; this takes its own lock).
func (b *Baselines) Observe(zone, metric string, value float64, now time.Time) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	key := baselineKey(zone, metric)
	buckets := b.stats[key]
	if buckets == nil {
		buckets = make(map[int]*baselineStat, 3)
		b.stats[key] = buckets
	}
	for _, h := range [2]int{now.Hour(), pooledHour} {
		st := buckets[h]
		if st == nil {
			st = &baselineStat{}
			buckets[h] = st
		}
		st.observe(value, baselineAlphaFloor)
	}
}

// scored is the result of scoring a reading against the learned model. mature reports
// whether a trusted (established) bucket backed the score — the caller must not raise a
// learned alarm on an immature baseline.
type scored struct {
	z      float64 // signed deviation in σ (positive = above the learned mean)
	mean   float64
	std    float64
	count  int
	hour   int // which bucket answered (clock hour, or pooledHour)
	mature bool
}

// score reads (no mutation) the best available bucket for (zone, metric) at now's hour:
// the clock-hour bucket if it is established, else the pooled bucket if established, else
// whatever exists (returned with mature=false so the caller falls back to a standard).
// Caller must hold b.mu.
func (b *Baselines) score(zone, metric string, value float64, now time.Time, spec metricSpec) scored {
	buckets := b.stats[baselineKey(zone, metric)]
	pick := func(h int) *baselineStat {
		if buckets == nil {
			return nil
		}
		return buckets[h]
	}
	hour := now.Hour()
	st, chosen := pick(hour), hour
	if st == nil || st.Count < baselineMature {
		if p := pick(pooledHour); p != nil {
			st, chosen = p, pooledHour
		}
	}
	if st == nil {
		return scored{}
	}
	sd := math.Max(st.std(), spec.minSigma)
	return scored{
		z:      (value - st.Mean) / sd,
		mean:   st.Mean,
		std:    st.std(),
		count:  st.Count,
		hour:   chosen,
		mature: st.Count >= baselineMature,
	}
}

// established reports whether some bucket for (zone, metric) is trusted — used by the
// automation to know if a learned load threshold exists yet. Caller must hold b.mu.
func (b *Baselines) establishedStat(zone, metric string, now time.Time) (*baselineStat, bool) {
	buckets := b.stats[baselineKey(zone, metric)]
	if buckets == nil {
		return nil, false
	}
	if st := buckets[now.Hour()]; st != nil && st.Count >= baselineMature {
		return st, true
	}
	if st := buckets[pooledHour]; st != nil && st.Count >= baselineMature {
		return st, true
	}
	return nil, false
}

// LoadThreshold returns the learned "this is a high load hour" line for a moment: the
// mean plus k·σ of the whole-building load baseline for that hour (pooled fallback). ok
// is false until the baseline has matured, so the caller keeps its fixed fallback until
// the model actually knows the building. Concurrency-safe.
func (b *Baselines) LoadThreshold(now time.Time, k float64) (threshold float64, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, ok := b.establishedStat("GLOBAL", "buildingLoadMw", now)
	if !ok {
		return 0, false
	}
	return st.Mean + k*math.Max(st.std(), metricSpecs["buildingLoadMw"].minSigma), true
}

// Coverage summarizes how much of the model is trusted vs still learning — the honest
// maturity readout the UI shows so a quiet recommendations list reads as "nothing
// abnormal" or "still warming up", never ambiguously. Concurrency-safe.
func (b *Baselines) Coverage() (established, learning int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, buckets := range b.stats {
		for h, st := range buckets {
			if h == pooledHour {
				continue // don't double-count the pooled shadow of each series
			}
			if st.Count >= baselineMature {
				established++
			} else {
				learning++
			}
		}
	}
	return
}

// --- persistence ------------------------------------------------------------

// Snapshot returns a deep copy of the model for JSON persistence, dropping buckets too
// immature to be worth keeping so the state file stays small. Concurrency-safe.
func (b *Baselines) Snapshot() map[string]map[int]*baselineStat {
	b.mu.Lock()
	defer b.mu.Unlock()
	const keepAbove = 5 // don't persist barely-seeded buckets
	out := make(map[string]map[int]*baselineStat, len(b.stats))
	for key, buckets := range b.stats {
		kept := make(map[int]*baselineStat)
		for h, st := range buckets {
			if st.Count > keepAbove {
				cp := *st
				kept[h] = &cp
			}
		}
		if len(kept) > 0 {
			out[key] = kept
		}
	}
	return out
}

// MarshalState / LoadState are the byte-level persistence surface main.go uses, so the
// internal baselineStat type never has to leave this package.
func (b *Baselines) MarshalState() ([]byte, error) {
	return json.Marshal(b.Snapshot())
}

func (b *Baselines) LoadState(data []byte) error {
	var snap map[string]map[int]*baselineStat
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}
	b.Restore(snap)
	return nil
}

// Restore loads a persisted snapshot at boot, replacing the (empty) model. Concurrency-safe.
func (b *Baselines) Restore(snap map[string]map[int]*baselineStat) {
	if snap == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stats = make(map[string]map[int]*baselineStat, len(snap))
	for key, buckets := range snap {
		cp := make(map[int]*baselineStat, len(buckets))
		for h, st := range buckets {
			s := *st
			cp[h] = &s
		}
		b.stats[key] = cp
	}
}
