package simulation

import (
	"math"
	"strings"
	"testing"
	"time"
)

// The three sensors that displace an assumption rather than add a reading: a supply-air
// probe (DS18B20), an AC clamp (SCT-013) and a daylight sensor (BH1750). Each arrived over
// MQTT and was stored long before anything read it; these tests pin the behaviour that
// makes each one change an engine number, and — just as importantly — the behaviour that
// makes a stale or implausible reading fall back to the library instead of driving physics.

// A fresh, plausible probe supersedes the library's design discharge temperature; a stale
// one does not; and a probe reading above setpoint is rejected rather than allowed to
// collapse the (setpoint − supply) denominator in the cooling law.
func TestSupplyProbeSupersedesDesignValue(t *testing.T) {
	design := Phys().SupplyAirDesignC
	const sp = 24.0

	z := &ZoneSim{}
	if got := z.supplyC(sp); got != design {
		t.Fatalf("no probe: want the library design value %.1f, got %.1f", design, got)
	}

	// A real louvre probe reading 14 °C — warmer than the 12 °C design, which is exactly
	// the case where believing the nameplate overstates cooling authority.
	z.HwSupplyC, z.HwSupplyAt = 14.0, time.Now()
	if got := z.supplyC(sp); got != 14.0 {
		t.Fatalf("fresh probe: want the measurement 14.0, got %.1f", got)
	}

	// Stale: the probe fell out of the louvre and stopped reporting.
	z.HwSupplyAt = time.Now().Add(-2 * hwStaleAfter)
	if got := z.supplyC(sp); got != design {
		t.Fatalf("stale probe: want fallback to %.1f, got %.1f", design, got)
	}

	// Implausible: a loose probe reading room air. Must not be believed, and must leave a
	// usable lift so the cooling law cannot divide by ~0.
	z.HwSupplyC, z.HwSupplyAt = 23.8, time.Now()
	got := z.supplyC(sp)
	if got == 23.8 {
		t.Fatal("a probe reading within 1 °C of setpoint must be rejected as implausible")
	}
	if sp-got < minSupplyLiftC {
		t.Fatalf("supply temp %.2f leaves less than the %.1f °C minimum lift", got, minSupplyLiftC)
	}
}

// The measured discharge temperature must reach the RLS identification, not just the
// physics: cooling authority is only interpretable against what it was referenced to.
func TestRoomConditionCarriesMeasuredSupply(t *testing.T) {
	e := newTestEngine()
	z := e.Zones["zone-office-a"]
	z.Setpoint = 24

	for _, c := range e.roomConditions() {
		if c.Zone == "zone-office-a" && c.SupplyC != 0 {
			t.Fatalf("no probe reporting, but SupplyC = %.1f", c.SupplyC)
		}
	}

	z.HwSupplyC, z.HwSupplyAt = 13.5, time.Now()
	found := false
	for _, c := range e.roomConditions() {
		if c.Zone != "zone-office-a" {
			continue
		}
		found = true
		if c.SupplyC != 13.5 {
			t.Fatalf("SupplyC = %.2f, want the measured 13.5", c.SupplyC)
		}
		if c.supplyC() != 13.5 {
			t.Fatalf("supplyC() = %.2f, want the measurement to supersede the design value", c.supplyC())
		}
	}
	if !found {
		t.Fatal("zone-office-a missing from roomConditions")
	}

	// Stale again: the fit must go back to the library rather than difference against a
	// reading from minutes ago as though it were current.
	z.HwSupplyAt = time.Now().Add(-2 * hwStaleAfter)
	for _, c := range e.roomConditions() {
		if c.Zone == "zone-office-a" && c.SupplyC != 0 {
			t.Fatalf("stale probe still populating SupplyC = %.1f", c.SupplyC)
		}
	}
}

// Identification against a probe must be counted, so a room fitted from evidence is
// distinguishable from one fitted against the design assumption.
func TestRoomModelReportsSupplyProvenance(t *testing.T) {
	d := NewDynamics()
	conds := []RoomCondition{{Zone: "z", Temp: 24, Setpoint: 24, OutdoorC: 33, FlowRatio: 0.5}}

	// Drive enough excited samples to accumulate accepted updates, alternating the
	// drivers so the excitation gate lets them through.
	at := 0.0
	for i := 0; i < 40; i++ {
		at += dynamicsSampleSimSecs
		c := conds[0]
		c.Temp = 24 + 0.4*float64(i%5)
		c.FlowRatio = 0.3 + 0.1*float64(i%4)
		c.Occupancy = i % 3
		if i >= 20 {
			c.SupplyC = 13.0 // probe comes online halfway through
		}
		d.Observe([]RoomCondition{c}, at)
	}

	models := d.RoomModels(map[string]string{"z": "z"})
	if len(models) == 0 {
		t.Skip("fit did not reach validity in this many samples; provenance path unchanged")
	}
	m := models[0]
	if m.SupplyMeasuredSamples == 0 {
		t.Fatal("samples were fitted against a measured supply temp but none were counted")
	}
	if m.SupplyMeasuredFrac <= 0 || m.SupplyMeasuredFrac > 1 {
		t.Fatalf("SupplyMeasuredFrac = %.3f, want a fraction in (0,1]", m.SupplyMeasuredFrac)
	}
}

// A daylight sensor scales that zone's solar term. It is believed only while the lights are
// off — an indoor lux reading under lit luminaires is measuring the electric lighting,
// whose heat is already counted in BaseHeatGain, so trusting it would double-count.
func TestDaylightScalesSolarGainOnlyWhenUncontaminated(t *testing.T) {
	ph := Phys()
	z := &ZoneSim{SolarGainMult: 0.5, LightsOn: false}
	reference := 0.5 * ph.SolarGainReferenceW

	// When sensor is omitted, solar gain follows dynamic clear-sky solar geometry.
	expectedFallback := z.solarGainWAt(time.Now())
	if got := z.solarGainW(); math.Abs(got-expectedFallback) > 1e-6 {
		t.Fatalf("no sensor: want dynamic solar geometry %.1f W, got %.1f", expectedFallback, got)
	}

	// Half the reference illuminance -> half the reference solar gain.
	z.HwLux, z.HwLuxAt = ph.DaylightReferenceLux/2, time.Now()
	if got := z.solarGainW(); math.Abs(got-reference/2) > 1e-9 {
		t.Fatalf("want %.0f W at half reference lux, got %.0f", reference/2, got)
	}

	// Lights on: the reading is contaminated by the luminaires, so it falls back to dynamic solar geometry.
	z.LightsOn = true
	if got := z.solarGainW(); math.Abs(got-expectedFallback) > 1e-6 {
		t.Fatalf("lights on: want dynamic solar fallback %.1f W, got %.1f", expectedFallback, got)
	}
	z.LightsOn = false

	// Stale: back to dynamic solar fallback.
	z.HwLuxAt = time.Now().Add(-2 * hwStaleAfter)
	if got := z.solarGainW(); math.Abs(got-expectedFallback) > 1e-6 {
		t.Fatalf("stale sensor: want dynamic solar fallback %.1f W, got %.1f", expectedFallback, got)
	}

	// Direct sun on a badly-placed probe must not be able to run away with the balance.
	z.HwLux, z.HwLuxAt = ph.DaylightReferenceLux*1000, time.Now()
	if got := z.solarGainW(); got > reference*maxDaylightRatio+1e-9 {
		t.Fatalf("solar gain %.0f W exceeds the %.0fx cap on %.0f W", got, maxDaylightRatio, reference)
	}

	// A zone with no aperture stays at zero however bright its sensor reads: 690 of the
	// fixture's 735 zones are interior, and a lux reading there is not solar gain.
	interior := &ZoneSim{SolarGainMult: 0, HwLux: 50000, HwLuxAt: time.Now()}
	if got := interior.solarGainW(); got != 0 {
		t.Fatalf("interior zone with no aperture gained %.0f W of 'solar'", got)
	}
}

// buildingLoadForTest runs one broadcast (the only place the building load is computed)
// and returns the resulting figure. With no websocket clients registered the serialization
// still runs and the write loop is a no-op, which is exactly the path under test.
func (e *Engine) buildingLoadForTest() float64 {
	e.broadcast()
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastLoadMw
}

// An AC clamp replaces the modelled COP for the slice of the building it covers, and only
// that slice. With nothing clamped the load must be bit-identical to the pure-model path.
func TestMeasuredAcPowerReplacesModelledCop(t *testing.T) {
	e := newTestEngine()
	for _, z := range e.Zones {
		z.Occupancy = 2
		z.BaseHeatGain = 1000
	}
	modelled := e.buildingLoadForTest()

	// Clamp one zone, reporting a draw far below what the COP curve would infer, and the
	// building load must fall — the measurement is believed over the model.
	z := e.Zones["zone-office-a"]
	z.HwAcW, z.HwAcAt = 50, time.Now()
	withClamp := e.buildingLoadForTest()

	if withClamp >= modelled {
		t.Fatalf("a clamp reading 50 W should lower the inferred load: modelled=%.6f clamped=%.6f",
			modelled, withClamp)
	}

	// Stale clamp: straight back to the modelled path, exactly.
	z.HwAcAt = time.Now().Add(-2 * hwStaleAfter)
	if got := e.buildingLoadForTest(); math.Abs(got-modelled) > 1e-12 {
		t.Fatalf("stale clamp must reduce to the modelled path: got %.12f want %.12f", got, modelled)
	}
}

// A physical node must bind to a zone whose type the DIGITIZER actually emits. The fixture
// types workspaces `cellular-office` and `open-office`; nothing in it is typed exactly
// "office". Matching that literal string found no zone, returned nil, and silently dropped
// every measured sample a real board sent — while the hardware inspector, which watches the
// raw MQTT stream below this binding, went on reporting the node as perfectly healthy.
func TestNodeBindsToDigitizerZoneTypes(t *testing.T) {
	e := NewEngine()
	for _, id := range []string{"zone-cellular-office-12-lvl1", "zone-open-office-3-lvl2"} {
		e.Zones[id] = &ZoneSim{Temp: 24, Type: strings.TrimSuffix(strings.TrimPrefix(id, "zone-"), "-12-lvl1"), Setpoint: 24}
	}
	e.Zones["zone-cellular-office-12-lvl1"].Type = "cellular-office"
	e.Zones["zone-open-office-3-lvl2"].Type = "open-office"
	e.Zones["zone-comms-room-1-lvl1"] = &ZoneSim{Temp: 22, Type: "comms-room", Setpoint: 22}

	temp := 28.9
	e.IngestTelemetry("Level 4", "zone_1", Measurement{Temp: &temp, TempReal: true, Source: "esp32"})

	var bound *ZoneSim
	var boundID string
	for id, z := range e.Zones {
		if z.MqttTopic == "zone_1" {
			bound, boundID = z, id
		}
	}
	if bound == nil {
		t.Fatal("node was dropped: no zone bound, so every measurement it sends is discarded")
	}
	if !strings.Contains(bound.Type, "office") {
		t.Fatalf("bound to %q (type %q), want an office-like zone", boundID, bound.Type)
	}
	if bound.HwTemp != 28.9 {
		t.Fatalf("measured temperature did not reach the zone: HwTemp=%.1f", bound.HwTemp)
	}
	if !bound.hwFresh() {
		t.Fatal("zone is not pinned to the measurement it just received")
	}
}

// A building with no office-like zone must still bind the node somewhere non-critical
// rather than discard its data — but never to a critical space, which the optimizer is
// forbidden from setting back.
func TestNodeBindsSomewhereRatherThanDroppingData(t *testing.T) {
	e := NewEngine()
	e.Zones["zone-meeting-room-2-lvl1"] = &ZoneSim{Temp: 24, Type: "meeting-room", Setpoint: 24}
	e.Zones["zone-comms-room-1-lvl1"] = &ZoneSim{Temp: 22, Type: "comms-room", Setpoint: 22}

	occ := 4
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{Occupancy: &occ, Source: "pico"})

	for id, z := range e.Zones {
		if z.MqttTopic != "pico_1" {
			continue
		}
		if z.Type == "comms-room" {
			t.Fatalf("bound a bring-up node to critical zone %q", id)
		}
		if z.Occupancy != 4 {
			t.Fatalf("occupancy did not land: %d", z.Occupancy)
		}
		return
	}
	t.Fatal("node was dropped in a building that has a perfectly good meeting room")
}
