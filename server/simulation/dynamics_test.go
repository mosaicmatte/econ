package simulation

// Tests for the learned room-dynamics model.
//
// The bar these tests hold the model to is system identification, not smoke: a room with
// KNOWN physical constants is simulated, the model sees only its telemetry, and the test
// asserts the model recovers the constants it was never told. That is the difference
// between "the code runs" and "the model actually learned the room".

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

// fakeRoom is a ground-truth room: the same first-order balance the model assumes, with
// coefficients the model has to discover. Rates are per hour, matching dynamics.go.
type fakeRoom struct {
	env, cool, occGain, base float64 // true thermal coefficients
	gen, ach, co2base        float64 // true CO2 coefficients
	temp, co2                float64
}

// step integrates the true room forward with fine substeps, so any error the test sees is
// the identifier's, not the test harness's.
func (r *fakeRoom) step(tout, flow float64, occ int, dtSec float64) {
	const sub = 40
	h := dtSec / 3600.0 / sub
	for i := 0; i < sub; i++ {
		dT := r.env*(tout-r.temp) + r.cool*flow*(r.temp-supplyAirC) +
			r.occGain*float64(occ) + r.base
		r.temp += dT * h
		dC := r.gen*float64(occ) + r.ach*(outdoorCo2Ppm-r.co2) + r.co2base
		r.co2 += dC * h
	}
}

// driveRoom runs a room for n samples with genuinely varying drivers and feeds the
// telemetry to the model. Excitation matters: a room held at constant flow and occupancy
// reveals nothing about which term is responsible for its behaviour, so the drivers are
// swept the way a real building's would be.
func driveRoom(d *Dynamics, r *fakeRoom, zone string, n int, co2Live bool) float64 {
	sim := 0.0
	for i := 0; i < n; i++ {
		// Deterministic but decorrelated drivers.
		flow := 0.25 + 0.7*(0.5+0.5*math.Sin(float64(i)/7.0))
		occ := int(9 + 9*math.Sin(float64(i)/4.3))
		if occ < 0 {
			occ = 0
		}
		tout := 31.0 + 3.0*math.Sin(float64(i)/23.0)

		sim += dynamicsSampleSimSecs
		r.step(tout, flow, occ, dynamicsSampleSimSecs)

		c := RoomCondition{
			Zone: zone, Label: zone, Temp: r.temp, Setpoint: 24.0,
			OutdoorC: tout, FlowRatio: flow, Occupancy: occ,
		}
		if co2Live {
			c.Co2, c.Co2Live = r.co2, true
		}
		d.Observe([]RoomCondition{c}, sim)
	}
	return sim
}

func healthyRoom() *fakeRoom {
	return &fakeRoom{
		env: 0.5, cool: -1.5, occGain: 0.05, base: 0.3,
		gen: 25, ach: 2.0, co2base: 0,
		temp: 24.0, co2: 500,
	}
}

// TestDynamicsIdentifiesKnownRoom is the core claim: the model recovers a room's thermal
// constants from telemetry alone.
func TestDynamicsIdentifiesKnownRoom(t *testing.T) {
	d := NewDynamics()
	r := healthyRoom()
	driveRoom(d, r, "zone-a", 500, false)

	models := d.RoomModels(map[string]string{"zone-a": "Room A"})
	if len(models) != 1 {
		t.Fatalf("expected one identified room, got %d", len(models))
	}
	m := models[0]
	if !m.ThermalReady {
		t.Fatalf("thermal model should be identified after 500 samples, got %+v", m)
	}

	// True time constant is 1/env hours = 2 h = 120 min.
	wantTau := 60.0 / r.env
	if math.Abs(m.TimeConstantMin-wantTau)/wantTau > 0.15 {
		t.Errorf("time constant: got %.1f min, want ~%.1f min", m.TimeConstantMin, wantTau)
	}
	// True cooling authority is -cool = 1.5.
	wantCool := -r.cool
	if math.Abs(m.CoolingAuthority-wantCool)/wantCool > 0.15 {
		t.Errorf("cooling authority: got %.3f, want ~%.3f", m.CoolingAuthority, wantCool)
	}
	// True per-occupant gain is 0.05 °C/h.
	if math.Abs(m.PerOccupantC-r.occGain) > 0.02 {
		t.Errorf("per-occupant gain: got %.4f, want ~%.4f", m.PerOccupantC, r.occGain)
	}
}

// TestDynamicsIdentifiesAirChangeRate is the same claim for ventilation: the model should
// discover how much air the room actually exchanges, which is the quantity that decides
// whether it can cope with its occupancy.
func TestDynamicsIdentifiesAirChangeRate(t *testing.T) {
	d := NewDynamics()
	r := healthyRoom()
	driveRoom(d, r, "zone-a", 500, true)

	m := d.RoomModels(nil)[0]
	if !m.Co2Ready {
		t.Fatalf("CO2 model should be identified, got %+v", m)
	}
	if math.Abs(m.AchPerHour-r.ach)/r.ach > 0.15 {
		t.Errorf("air-change rate: got %.2f ACH, want ~%.2f ACH", m.AchPerHour, r.ach)
	}
	if math.Abs(m.PerOccupantPpm-r.gen)/r.gen > 0.20 {
		t.Errorf("per-occupant CO2: got %.1f ppm/h, want ~%.1f ppm/h", m.PerOccupantPpm, r.gen)
	}
}

// TestDynamicsImmatureIsNotTrusted guards the honesty discipline: a barely-observed room
// must not be allowed to predict anything.
func TestDynamicsImmatureIsNotTrusted(t *testing.T) {
	d := NewDynamics()
	r := healthyRoom()
	driveRoom(d, r, "zone-a", 10, true) // well under dynamicsMature

	for _, m := range d.RoomModels(nil) {
		if m.ThermalReady || m.Co2Ready {
			t.Fatalf("a 10-sample room must not be reported as identified: %+v", m)
		}
	}
	look := d.ThermalOutlookFor(RoomCondition{Zone: "zone-a", Temp: 25, OutdoorC: 33, FlowRatio: 0.5}, 25, 1800)
	if look.Ok {
		t.Error("an immature model must not return a usable outlook")
	}
	if ident, learning := d.Coverage(); ident != 0 || learning != 1 {
		t.Errorf("coverage should report 0 identified / 1 learning, got %d/%d", ident, learning)
	}
}

// TestDynamicsPredictsActualCrossing checks the prediction against the truth it is
// predicting: the model's estimated time-to-breach should match when the real room
// actually crosses the limit.
func TestDynamicsPredictsActualCrossing(t *testing.T) {
	d := NewDynamics()
	r := healthyRoom()
	driveRoom(d, r, "zone-a", 600, false)

	// Park the room in a state that is heading up: low flow, hot outside, busy.
	const flow, tout, occ = 0.15, 34.0, 16
	r.temp = 23.5
	limit := 25.0

	cond := RoomCondition{
		Zone: "zone-a", Temp: r.temp, Setpoint: 24.0,
		OutdoorC: tout, FlowRatio: flow, Occupancy: occ,
	}
	look := d.ThermalOutlookFor(cond, limit, 1800)
	if !look.Ok || look.SecsToLimit <= 0 {
		t.Fatalf("expected a predicted crossing, got %+v", look)
	}

	// Now run the true room under exactly those drivers and time the real crossing.
	actual := 0.0
	for actual < 4*3600 && r.temp < limit {
		r.step(tout, flow, occ, 10)
		actual += 10
	}
	if r.temp < limit {
		t.Fatalf("ground-truth room never crossed %.1f°C; test setup is wrong", limit)
	}
	if rel := math.Abs(look.SecsToLimit-actual) / actual; rel > 0.20 {
		t.Errorf("predicted crossing in %.0fs, actually crossed in %.0fs (%.0f%% off)",
			look.SecsToLimit, actual, rel*100)
	}
}

// weakRoom cannot hold setpoint: its cooling authority is too low for its envelope and
// load, so even wide open it settles above 24°C.
func weakRoom() *fakeRoom {
	return &fakeRoom{
		env: 0.5, cool: -0.25, occGain: 0.05, base: 0.3,
		gen: 40, ach: 0.5, co2base: 0,
		temp: 26.0, co2: 700,
	}
}

// TestCapabilityShortfallIsDetected is the finding that no threshold on a reading can
// produce: the room is not merely hot, it is incapable of being held, and the model knows
// because it learned how much cooling this room's VAV actually delivers.
func TestCapabilityShortfallIsDetected(t *testing.T) {
	d := NewDynamics()
	r := weakRoom()
	driveRoom(d, r, "zone-weak", 600, false)

	cond := RoomCondition{
		Zone: "zone-weak", Label: "Weak Room", Temp: r.temp, Setpoint: 24.0,
		OutdoorC: 33.0, FlowRatio: 0.6, Occupancy: 10,
	}
	recs := d.PredictiveRecommendations([]RoomCondition{cond}, time.Now())

	var got *Recommendation
	for i := range recs {
		if recs[i].Kind == "capability" {
			got = &recs[i]
		}
	}
	if got == nil {
		t.Fatalf("expected a capability-shortfall recommendation, got %+v", recs)
	}
	if got.Basis != "learned" {
		t.Errorf("capability finding must be learned-basis, got %q", got.Basis)
	}
	if got.Equilibrium <= cond.Setpoint {
		t.Errorf("full-flow equilibrium %.1f should exceed setpoint %.1f", got.Equilibrium, cond.Setpoint)
	}
	if got.Action != "cool" {
		t.Errorf("capability finding should carry a real action, got %q", got.Action)
	}
}

// TestVentilationPredictionUsesLearnedAch proves the CO2 prediction is driven by the
// room's identified air-change rate: an under-ventilated room busy enough to exceed the
// guideline is flagged before it gets there.
func TestVentilationPredictionUsesLearnedAch(t *testing.T) {
	d := NewDynamics()
	r := weakRoom()
	driveRoom(d, r, "zone-weak", 600, true)

	m := d.RoomModels(nil)[0]
	if !m.Co2Ready {
		t.Fatalf("expected an identified CO2 model, got %+v", m)
	}
	if math.Abs(m.AchPerHour-r.ach)/r.ach > 0.20 {
		t.Errorf("learned ACH %.2f should be near the true %.2f", m.AchPerHour, r.ach)
	}

	// 20 people in a 0.5 ACH room: the balance settles far past the guideline.
	cond := RoomCondition{
		Zone: "zone-weak", Label: "Weak Room", Temp: 24, Setpoint: 24,
		OutdoorC: 33, FlowRatio: 0.6, Occupancy: 20,
		Co2: 800, Co2Live: true,
	}
	look := d.Co2OutlookFor(cond, 1000, 1800)
	if !look.Ok || look.Equilibrium <= 1000 {
		t.Fatalf("expected the learned balance to settle past the guideline, got %+v", look)
	}

	recs := d.PredictiveRecommendations([]RoomCondition{cond}, time.Now())
	var got *Recommendation
	for i := range recs {
		if recs[i].Metric == "co2" && recs[i].Kind == "prediction" {
			got = &recs[i]
		}
	}
	if got == nil {
		t.Fatalf("expected a predictive CO2 recommendation, got %+v", recs)
	}
	if got.Action != "purge" {
		t.Errorf("ventilation prediction should map to the purge action, got %q", got.Action)
	}
	if got.EtaSec <= 0 {
		t.Errorf("prediction should carry a time-to-breach, got %.0f", got.EtaSec)
	}
}

// TestDynamicsPersistenceRoundTrip: a restart must not throw away the identification.
func TestDynamicsPersistenceRoundTrip(t *testing.T) {
	d := NewDynamics()
	r := healthyRoom()
	driveRoom(d, r, "zone-a", 400, true)

	before := d.RoomModels(nil)[0]
	blob, err := d.MarshalState()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	restored := NewDynamics()
	if err := restored.LoadState(blob); err != nil {
		t.Fatalf("load: %v", err)
	}
	after := restored.RoomModels(nil)[0]

	if !after.ThermalReady || !after.Co2Ready {
		t.Fatalf("restored model lost its identification: %+v", after)
	}
	if math.Abs(after.TimeConstantMin-before.TimeConstantMin) > 1e-6 ||
		math.Abs(after.AchPerHour-before.AchPerHour) > 1e-9 {
		t.Errorf("restored constants differ: before %+v after %+v", before, after)
	}
	// The restored model must not difference its first new sample against a stale one.
	restored.Observe([]RoomCondition{{Zone: "zone-a", Temp: 40, OutdoorC: 33, FlowRatio: 1}}, 999999)
	if n := restored.RoomModels(nil)[0].ThermalSamples; n != after.ThermalSamples {
		t.Errorf("first post-restore sample must not be differenced across the gap (%d -> %d)",
			after.ThermalSamples, n)
	}
}

// TestLoadStateRejectsCorruptModel: a hand-edited or truncated state file must be
// discarded, not run.
func TestLoadStateRejectsCorruptModel(t *testing.T) {
	d := NewDynamics()
	bad := `{"zone-x":{"thermal":{"theta":[1,2],"p":[1],"n":900},"co2":null}}`
	if err := d.LoadState([]byte(bad)); err != nil {
		t.Fatalf("a corrupt entry should be skipped, not error: %v", err)
	}
	if ident, learning := d.Coverage(); ident != 0 || learning != 0 {
		t.Errorf("corrupt model should have been discarded, got %d/%d", ident, learning)
	}
}

// TestIdentificationSurvivesClosedLoopOperation is a regression test for a failure found
// by running the real thing, not by reasoning about it.
//
// A five-hour live soak took the twin from 1152 identified rooms to 0. The cause was not a
// crash or a bad reading: it was that a building under closed-loop control is a poor
// subject for system identification. The optimizer drives airflow as a function of
// temperature error, so the cooling regressor becomes nearly a linear combination of the
// envelope regressor and the constant, and least squares on collinear regressors is
// under-determined — it splits the coefficient arbitrarily and still fits the residuals.
// The live fits ended up with NEGATIVE envelope coupling and POSITIVE cooling: a model of a
// room that heats up when you cool it. The sanity gate caught it and correctly reported
// zero identified rooms, which is the honest outcome but not a useful one.
//
// This reproduces exactly that regime — a controller closing the loop on temperature — and
// asserts the identification is both KEPT and still correct.
func TestIdentificationSurvivesClosedLoopOperation(t *testing.T) {
	d := NewDynamics()
	r := healthyRoom()
	driveRoom(d, r, "zone-a", 500, true) // identified from genuine transients first

	before := d.RoomModels(nil)[0]
	if !before.ThermalReady || !before.Co2Ready {
		t.Fatalf("setup: room should be identified before closed-loop operation, got %+v", before)
	}

	// Now run it the way a real building runs: a proportional controller parks the room at
	// setpoint, so flow is a deterministic function of temperature error. This is what
	// makes the regressors collinear.
	//
	// Sensor noise is essential to the reproduction, and is why an earlier noise-free
	// version of this test failed to catch anything. With the regressors frozen, every
	// residual is pure noise — but exponential forgetting keeps the estimator gain alive,
	// so θ random-walks instead of holding still. That is the actual erosion mechanism.
	sim := 500 * dynamicsSampleSimSecs
	rng := rand.New(rand.NewSource(7))
	const setpoint, tout, occ = 24.0, 32.0, 8
	for i := 0; i < 8000; i++ {
		flow := 0.5 + 0.6*(r.temp-setpoint) // the closed loop
		if flow < 0 {
			flow = 0
		} else if flow > 1 {
			flow = 1
		}
		sim += dynamicsSampleSimSecs
		r.step(tout, flow, occ, dynamicsSampleSimSecs)
		d.Observe([]RoomCondition{{
			Zone: "zone-a", Temp: r.temp + rng.NormFloat64()*0.05, Setpoint: setpoint,
			OutdoorC: tout, FlowRatio: flow, Occupancy: occ,
			Co2: r.co2 + rng.NormFloat64()*8, Co2Live: true,
		}}, sim)
	}

	after := d.RoomModels(nil)[0]
	if !after.ThermalReady {
		t.Fatalf("thermal identification was lost under closed-loop control: theta=%v", after.ThermalTheta)
	}
	if !after.Co2Ready {
		t.Fatalf("CO2 identification was lost under closed-loop control: theta=%v", after.Co2Theta)
	}
	// Signs must still be physical — this is exactly what the live run violated.
	if after.ThermalTheta[0] <= 0 {
		t.Errorf("envelope coupling went non-positive (%.4g): the room would cool as it gets hotter outside", after.ThermalTheta[0])
	}
	if after.ThermalTheta[1] >= 0 {
		t.Errorf("cooling coefficient went non-negative (%.4g): the model says cooling heats the room", after.ThermalTheta[1])
	}
	// And it must still be the RIGHT model, not merely a plausible-looking one.
	wantTau := 60.0 / r.env
	if math.Abs(after.TimeConstantMin-wantTau)/wantTau > 0.35 {
		t.Errorf("time constant drifted under closed-loop control: %.1f min, true %.1f min",
			after.TimeConstantMin, wantTau)
	}
	if math.Abs(after.AchPerHour-r.ach)/r.ach > 0.35 {
		t.Errorf("air-change rate drifted under closed-loop control: %.2f, true %.2f",
			after.AchPerHour, r.ach)
	}
	if ident, _ := d.Coverage(); ident != 1 {
		t.Errorf("coverage should still report the room as identified, got %d", ident)
	}
}

// TestExtrapolatedOutlookIsClampedAndFlagged: a linear fit asked about a regime it never
// saw will happily return an absurd asymptote. The crossing time stays usable, but the
// settling point must be clamped to what the twin can physically represent and flagged, so
// the UI reports a direction of travel rather than a fabricated number.
func TestExtrapolatedOutlookIsClampedAndFlagged(t *testing.T) {
	d := NewDynamics()
	r := healthyRoom()
	driveRoom(d, r, "zone-a", 500, false)

	// Almost no airflow, very hot outside, packed room: far outside anything identified.
	cond := RoomCondition{
		Zone: "zone-a", Temp: 24, Setpoint: 24,
		OutdoorC: 44, FlowRatio: 0.01, Occupancy: 120,
	}
	look := d.ThermalOutlookFor(cond, 25, 1800)
	if !look.Ok {
		t.Fatal("expected a usable outlook")
	}
	if look.Equilibrium > physMaxC || look.Predicted > physMaxC {
		t.Errorf("outlook must be clamped to the physical range, got eq=%.1f pred=%.1f",
			look.Equilibrium, look.Predicted)
	}
	if !look.Extrapolated {
		t.Error("an equilibrium far outside the identified regime must be flagged as extrapolated")
	}
	// The crossing time is the actionable part and must still be produced.
	if look.SecsToLimit <= 0 {
		t.Errorf("expected a usable crossing time even when extrapolating, got %.0f", look.SecsToLimit)
	}

	// A normal operating point must NOT be flagged.
	normal := RoomCondition{Zone: "zone-a", Temp: 24, Setpoint: 24, OutdoorC: 33, FlowRatio: 0.6, Occupancy: 8}
	if d.ThermalOutlookFor(normal, 25, 1800).Extrapolated {
		t.Error("an ordinary operating point must not be flagged as extrapolated")
	}
}

// TestLearnedSetbackDiffersPerRoom: the vacancy setback used to be a flat +4 °C for every
// zone in the building. The right depth is a property of the room — how fast it recovers —
// so a responsive room should earn a deeper setback than a sluggish one, and both should
// come out of their own identified physics rather than a constant.
func TestLearnedSetbackDiffersPerRoom(t *testing.T) {
	// A responsive room: strong cooling authority, so it catches up quickly.
	quick := NewDynamics()
	qr := healthyRoom()
	driveRoom(quick, qr, "quick", 500, false)

	// A sluggish room: the same envelope, much less cooling authority — but still enough
	// to reach setpoint, otherwise it is refused outright (see the next test).
	slow := NewDynamics()
	sr := healthyRoom()
	sr.cool = -0.8
	driveRoom(slow, sr, "slow", 500, false)

	const setpoint, outdoor, budget = 24.0, 33.0, 1800.0
	qDelta, qOk := quick.SetbackCeiling("quick", setpoint, outdoor, budget)
	sDelta, sOk := slow.SetbackCeiling("slow", setpoint, outdoor, budget)
	if !qOk || !sOk {
		t.Fatalf("both rooms should be identified: quick=%v slow=%v", qOk, sOk)
	}
	if qDelta <= sDelta {
		t.Errorf("the faster-recovering room should earn the deeper setback: quick=%.2f slow=%.2f",
			qDelta, sDelta)
	}
	for name, d := range map[string]float64{"quick": qDelta, "slow": sDelta} {
		if d < setbackMinC || d > setbackMaxC {
			t.Errorf("%s setback %.2f is outside the safe band [%.1f, %.1f]",
				name, d, setbackMinC, setbackMaxC)
		}
	}

	// A longer recovery budget must never yield a SHALLOWER setback.
	long, _ := quick.SetbackCeiling("quick", setpoint, outdoor, 3600)
	if long < qDelta {
		t.Errorf("a longer recovery budget should allow at least as deep a setback: %.2f < %.2f",
			long, qDelta)
	}

	// An unidentified room must fall back rather than invent a depth.
	if _, ok := quick.SetbackCeiling("no-such-room", setpoint, outdoor, budget); ok {
		t.Error("an unidentified room must not produce a learned setback")
	}
}

// TestSetbackRefusedWhenRoomCannotRecover: a room that cannot reach its setpoint even at
// full cooling has no recovery margin to spend, so it must not be set back on the strength
// of a calculation that assumes it can catch up.
func TestSetbackRefusedWhenRoomCannotRecover(t *testing.T) {
	d := NewDynamics()
	r := weakRoom()
	driveRoom(d, r, "weak", 600, false)

	// 24 °C setpoint on a hot day is beyond this room's full-flow equilibrium.
	if delta, ok := d.SetbackCeiling("weak", 24.0, 36.0, 1800); ok {
		t.Errorf("a room that cannot hold setpoint must not be given a learned setback, got %.2f", delta)
	}
}

// TestMergeDropsDuplicatePrediction: when a room is ALREADY anomalous on a metric, the
// forecast for that same metric is redundant and must not double-report it.
func TestMergeDropsDuplicatePrediction(t *testing.T) {
	var rep RecommendationReport
	rep.Recommendations = []Recommendation{
		{Id: "temp:z1", Zone: "z1", Metric: "temp", Kind: "anomaly", Score: 6},
	}
	extra := []Recommendation{
		{Id: "predict-temp:z1", Zone: "z1", Metric: "temp", Kind: "prediction", Score: 7},
		{Id: "predict-co2:z1", Zone: "z1", Metric: "co2", Kind: "prediction", Score: 5},
	}
	out := mergeRecommendations(rep, extra, 10)
	if len(out.Recommendations) != 2 {
		t.Fatalf("expected the duplicate temp prediction to be dropped, got %+v", out.Recommendations)
	}
	for _, r := range out.Recommendations {
		if r.Id == "predict-temp:z1" {
			t.Error("the temp prediction duplicated a present temp anomaly and should be gone")
		}
	}
	// Ranking must still be score-ordered across the merged set.
	if out.Recommendations[0].Score < out.Recommendations[1].Score {
		t.Error("merged list must be ranked most-severe first")
	}
}
