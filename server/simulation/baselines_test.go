package simulation

import (
	"testing"
	"time"
)

// A bucket learns its metric's normal, and a reading far from that normal scores as a
// large deviation — while a reading near the mean scores near zero. This is the whole
// point: the anomaly is measured against what the zone actually does, not a fixed number.
func TestBaselineLearnsAndScores(t *testing.T) {
	b := NewBaselines()
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)

	// Feed a stable ~620 ppm with mild jitter until the bucket matures.
	jitter := []float64{-30, 10, -10, 25, -20, 15, 0, -15, 20, -5}
	for i := 0; i < baselineMature+10; i++ {
		b.Observe("zone-office-a", "co2", 620+jitter[i%len(jitter)], at)
	}

	spec := metricSpecs["co2"]
	b.mu.Lock()
	near := b.score("zone-office-a", "co2", 630, at, spec)
	far := b.score("zone-office-a", "co2", 1150, at, spec)
	b.mu.Unlock()

	if !near.mature || !far.mature {
		t.Fatalf("bucket should be mature after %d observations", baselineMature+10)
	}
	if near.mean < 590 || near.mean > 650 {
		t.Fatalf("learned mean should sit near 620, got %.1f", near.mean)
	}
	if near.z < -1.5 || near.z > 1.5 {
		t.Fatalf("a reading near the mean must score near zero, got %.2fσ", near.z)
	}
	if far.z < spec.zAlert {
		t.Fatalf("a 1150 ppm reading against a ~620 normal must exceed the alert threshold, got %.2fσ", far.z)
	}
}

// An immature bucket is never trusted to alarm: score reports mature=false so the caller
// falls back to a recognized standard instead of firing on noise.
func TestBaselineImmatureNotTrusted(t *testing.T) {
	b := NewBaselines()
	at := time.Date(2026, 7, 21, 9, 0, 0, 0, time.Local)
	for i := 0; i < baselineMature-5; i++ {
		b.Observe("zone-office-a", "co2", 600, at)
	}
	b.mu.Lock()
	sc := b.score("zone-office-a", "co2", 2000, at, metricSpecs["co2"])
	b.mu.Unlock()
	if sc.mature {
		t.Fatalf("a bucket with %d < %d samples must not be trusted", baselineMature-5, baselineMature)
	}
}

// When the current clock hour has no mature bucket but the pooled (all-hours) bucket does,
// scoring falls back to pooled — the demo-friendly cold path that is still learned.
func TestBaselineHourFallbackToPooled(t *testing.T) {
	b := NewBaselines()
	// Feed many different hours so the pooled bucket matures but no single hour does.
	for i := 0; i < baselineMature*3; i++ {
		h := time.Date(2026, 7, 21, i%24, 0, 0, 0, time.Local)
		b.Observe("zone-office-a", "temp", 24.0, h)
	}
	// Score at an hour whose own bucket has only ~few samples.
	at := time.Date(2026, 7, 22, 3, 30, 0, 0, time.Local)
	b.mu.Lock()
	sc := b.score("zone-office-a", "temp", 24.0, at, metricSpecs["temp"])
	b.mu.Unlock()
	if !sc.mature {
		t.Fatal("pooled bucket should back the score when the clock-hour bucket is immature")
	}
	if sc.hour != pooledHour {
		t.Fatalf("expected the pooled bucket to answer, got hour=%d", sc.hour)
	}
}

// The learned CO2 baseline drives a learned recommendation; before it matures, the same
// high reading produces the recognized-standard recommendation instead — labelled as such.
func TestRecommendLearnedVsStandardBasis(t *testing.T) {
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)
	reading := []ZoneReading{{
		Zone: "zone-office-a", Label: "office-a", Type: "office",
		Temp: 24, Setpoint: 24, Co2: 1200, Co2Live: true,
	}}

	// Cold start: no baseline yet, but 1200 > 1000 ppm → standard-basis recommendation.
	cold := NewBaselines()
	rep := cold.Recommend(reading, 2.0, at, 10)
	if len(rep.Recommendations) != 1 || rep.Recommendations[0].Basis != "standard" {
		t.Fatalf("cold start must fall back to the ASHRAE standard, got %+v", rep.Recommendations)
	}

	// Warm: teach it a ~600 ppm normal, then 1200 ppm is a learned anomaly.
	warm := NewBaselines()
	for i := 0; i < baselineMature+5; i++ {
		warm.Observe("zone-office-a", "co2", 600, at)
	}
	rep = warm.Recommend(reading, 2.0, at, 10)
	if len(rep.Recommendations) == 0 {
		t.Fatal("a 1200 ppm reading against a learned 600 normal must be recommended")
	}
	r := rep.Recommendations[0]
	if r.Basis != "learned" {
		t.Fatalf("an established baseline must produce a learned-basis recommendation, got %q", r.Basis)
	}
	if r.Deviation < metricSpecs["co2"].zAlert {
		t.Fatalf("learned deviation should exceed the alert threshold, got %.2fσ", r.Deviation)
	}
	if r.Action != "purge" {
		t.Fatalf("a CO2 recommendation must carry the real purge action, got %q", r.Action)
	}
}

// A hot zone that is at or below setpoint is being cooled correctly and must NOT be flagged,
// even if its temperature is high — the direction/setpoint gate, not a bare threshold.
func TestRecommendTempRespectsSetpoint(t *testing.T) {
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)
	b := NewBaselines()
	for i := 0; i < baselineMature+5; i++ {
		b.Observe("zone-office-a", "temp", 22.0, at)
	}
	// 26°C is well above the 22 learned normal, but the setpoint is 27 — cooling is on track.
	rep := b.Recommend([]ZoneReading{{
		Zone: "zone-office-a", Label: "office-a", Temp: 26.0, Setpoint: 27.0,
	}}, 2.0, at, 10)
	for _, r := range rep.Recommendations {
		if r.Metric == "temp" {
			t.Fatalf("a zone below its setpoint must not raise a temp recommendation, got %+v", r)
		}
	}
}

// The learned load threshold only exists once the building-load baseline has matured, and
// then sits above the learned mean — so the pre-cool automation stays conventional until
// the model actually knows the building, then triggers on genuinely-high forecasts.
func TestLoadThresholdMaturity(t *testing.T) {
	b := NewBaselines()
	at := time.Date(2026, 7, 21, 17, 0, 0, 0, time.Local)

	if _, ok := b.LoadThreshold(at, 1.5); ok {
		t.Fatal("no baseline yet: threshold must be unavailable")
	}
	for i := 0; i < baselineMature+5; i++ {
		b.Observe("GLOBAL", "buildingLoadMw", 1.8, at)
	}
	thr, ok := b.LoadThreshold(at, 1.5)
	if !ok {
		t.Fatal("threshold must be available once the load baseline matures")
	}
	if thr <= 1.8 {
		t.Fatalf("learned threshold must sit above the mean load, got %.2f", thr)
	}
}

// End to end through the Engine: a sensor-bound zone reading far above its learned CO2
// normal produces a learned recommendation carrying the real purge action — proving the
// gather-under-e.mu / score-under-baselines.mu path is wired without a lock cycle.
func TestEngineRecommendationsEndToEnd(t *testing.T) {
	e := newTestEngine()
	at := time.Now()

	// Teach this zone a calm ~600 ppm normal.
	for i := 0; i < baselineMature+5; i++ {
		e.baselines.Observe("zone-office-a", "co2", 600, at)
	}
	// Now the zone's live NDIR reads 1300 ppm (fresh).
	z := e.Zones["zone-office-a"]
	z.HwCo2 = 1300
	z.HwCo2At = time.Now()

	rep := e.Recommendations(8)
	var got *Recommendation
	for i := range rep.Recommendations {
		if rep.Recommendations[i].Zone == "zone-office-a" && rep.Recommendations[i].Metric == "co2" {
			got = &rep.Recommendations[i]
		}
	}
	if got == nil {
		t.Fatalf("expected a CO2 recommendation for the sensor-bound zone, got %+v", rep.Recommendations)
	}
	if got.Basis != "learned" {
		t.Fatalf("an established baseline must produce a learned recommendation, got %q", got.Basis)
	}
	if got.Action != "purge" {
		t.Fatalf("recommendation must carry the real purge action, got %q", got.Action)
	}
	if rep.Model.Established == 0 {
		t.Fatal("model coverage must report at least one established bucket")
	}
}

// The model survives a restart: a snapshot round-trips through Restore with its learned
// distributions intact, so avoided re-learning is real.
func TestBaselinePersistRoundTrip(t *testing.T) {
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)
	src := NewBaselines()
	for i := 0; i < baselineMature+5; i++ {
		src.Observe("zone-office-a", "co2", 700, at)
	}
	snap := src.Snapshot()
	if len(snap) == 0 {
		t.Fatal("snapshot must contain the matured bucket")
	}

	dst := NewBaselines()
	dst.Restore(snap)
	dst.mu.Lock()
	sc := dst.score("zone-office-a", "co2", 700, at, metricSpecs["co2"])
	dst.mu.Unlock()
	if !sc.mature || sc.mean < 680 || sc.mean > 720 {
		t.Fatalf("restored baseline must retain its learned mean, got mean=%.1f mature=%v", sc.mean, sc.mature)
	}
}
