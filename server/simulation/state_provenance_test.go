package simulation

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// State that describes ONE building must not be restored into a different one.
//
// This has now been the same bug four times — the recorded load series, the whole-building
// learned baselines, the plug savings counter, and the identified room models — so it gets
// a test rather than a fourth fix and a hope. The failure is quiet by construction: every
// one of these files parses cleanly and produces plausible numbers, they are simply the
// wrong building's numbers, and the consequences ranged from a misleading dashboard figure
// to a pre-cool window actually opening on a forecast anchored to another building.
//
// Zone ids do not save you. `zone-office-lvl1` and `zone-bathroom-lvl1` are what ANY house
// digitized by tools/housify_fixture.py produces, so per-zone keys collide across genuinely
// different buildings.

const houseA = `{
  "buildingId": "bldg-a",
  "floors": [{"height": 2.8, "zones": [
    {"zoneId":"zone-office-lvl1","zoneType":"home-office","volume":60,
     "thermalProperties":{"setpoint":26,"deadband":2,"baseHeatLoad":300},
     "hvacMapping":{"vavId":"vav-office-lvl1"}}
  ]}]
}`

const houseB = `{
  "buildingId": "bldg-b",
  "floors": [{"height": 2.8, "zones": [
    {"zoneId":"zone-office-lvl1","zoneType":"home-office","volume":60,
     "thermalProperties":{"setpoint":26,"deadband":2,"baseHeatLoad":300},
     "hvacMapping":{"vavId":"vav-office-lvl1"}}
  ]}]
}`

func engineFor(t *testing.T, fixture string) *Engine {
	t.Helper()
	e := &Engine{
		Zones: map[string]*ZoneSim{}, Vavs: map[string]*VavSim{},
		lastCmd: map[string]string{}, demoAssign: map[string]string{},
		PMax: 600.0, KFan: 0.01,
		baselines: NewBaselines(), dynamics: NewDynamics(),
	}
	if err := e.buildFromJSON([]byte(fixture)); err != nil {
		t.Fatalf("build: %v", err)
	}
	return e
}

func TestLoadHistoryIsNotRestoredIntoAnotherBuilding(t *testing.T) {
	a := engineFor(t, houseA)
	a.loadHist = []float64{1.5, 1.6, 1.7}
	saved, err := a.MarshalLoadHistory()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	b := engineFor(t, houseB)
	if err := b.LoadLoadHistory(saved); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(b.LoadHistory()); got != 0 {
		t.Errorf("building B restored %d load samples from building A; the zero-shot "+
			"forecaster would forecast B from A's megawatts", got)
	}

	// Same building: it must still restore, or the forecaster loses its context on a
	// routine restart, which is the reason the file exists.
	a2 := engineFor(t, houseA)
	if err := a2.LoadLoadHistory(saved); err != nil {
		t.Fatalf("load same: %v", err)
	}
	if got := len(a2.LoadHistory()); got != 3 {
		t.Errorf("same building restored %d of 3 samples; a restart must not cost the context", got)
	}
}

func TestGlobalBaselinesAreNotRestoredIntoAnotherBuilding(t *testing.T) {
	a := engineFor(t, houseA)
	now := time.Now()
	for i := 0; i < baselineMature+5; i++ {
		a.baselines.Observe("GLOBAL", "buildingLoadMw", 0.6, now)
		a.baselines.Observe("zone-office-lvl1", "temp", 25.0, now)
	}
	saved, err := a.MarshalBaselines()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	b := engineFor(t, houseB)
	if err := b.LoadBaselines(saved); err != nil {
		t.Fatalf("load: %v", err)
	}
	// GLOBAL is the key that is identical in every building, and it is what the pre-cool
	// automation triggers against.
	if _, ok := b.baselines.LoadThreshold(now, 1.0); ok {
		t.Error("building B inherited building A's whole-building load baseline; the " +
			"pre-cool trigger would be A's 0.6 MW")
	}
}

func TestPlugSavingsAndRoomModelsCarryTheirBuilding(t *testing.T) {
	a := engineFor(t, houseA)

	// Room models: every key here can collide, not just GLOBAL.
	saved, err := a.MarshalDynamics()
	if err != nil {
		t.Fatalf("marshal dynamics: %v", err)
	}
	var doc struct {
		BuildingId string `json:"buildingId"`
	}
	if json.Unmarshal(saved, &doc) != nil || doc.BuildingId != "bldg-a" {
		t.Fatalf("identified room models must record the building they were identified in, got %q", doc.BuildingId)
	}
	b := engineFor(t, houseB)
	if err := b.LoadDynamics(saved); err != nil {
		t.Fatalf("load dynamics: %v", err)
	}
	if ident, learning := b.DynamicsCoverage(); ident != 0 || learning != 0 {
		t.Errorf("building B inherited %d identified / %d learning rooms from A", ident, learning)
	}

	// And the engine must be able to say which building it is, since every guard above
	// depends on it.
	if a.BuildingId() != "bldg-a" || b.BuildingId() != "bldg-b" {
		t.Errorf("BuildingId not tracked: %q / %q", a.BuildingId(), b.BuildingId())
	}
}

func TestLegacyUntaggedStateIsRefusedWhenTheBuildingIsKnown(t *testing.T) {
	// Files written before provenance existed carry no id. They cannot prove they describe
	// the current building, so a building that knows its own name must not trust them.
	legacyHist := []byte(`[1.1, 1.2, 1.3]`)
	e := engineFor(t, houseA)
	if err := e.LoadLoadHistory(legacyHist); err != nil {
		t.Fatalf("legacy load: %v", err)
	}
	if got := len(e.LoadHistory()); got != 0 {
		t.Errorf("untagged history restored %d samples into a building that declares an id", got)
	}

	legacyRooms := []byte(`{"zone-office-lvl1":{}}`)
	if err := e.LoadDynamics(legacyRooms); err != nil {
		t.Fatalf("legacy dynamics: %v", err)
	}
	if ident, learning := e.DynamicsCoverage(); ident != 0 || learning != 0 {
		t.Errorf("untagged room models restored %d/%d into a building that declares an id", ident, learning)
	}
}

func TestVavsAreSizedFromTheirZoneNotTheZoneCount(t *testing.T) {
	// The defect this pins: every box was created at Resistance 1.0, so the network solve
	// shared a fixed fan capacity evenly and per-box flow depended only on how MANY boxes
	// existed. Two boxes in a house each drew 21.9 m³/s — 150,000 m³/h into 72 m².
	e := engineFor(t, houseA)
	v := e.Vavs["vav-office-lvl1"]
	if v == nil {
		t.Fatal("no VAV built for the zone")
	}
	e.doHardyCross()

	// 60 m³ at the library's design ACH.
	want := 60.0 * Phys().SupplyAirDesignAch / 3600.0
	if got := v.Flow; got < want*0.8 || got > want*1.25 {
		t.Errorf("design flow = %.4f m³/s, want ≈%.4f (zone volume × %.1f ACH)",
			got, want, Phys().SupplyAirDesignAch)
	}
	if v.DesignResistance <= 1.0 {
		t.Errorf("DesignResistance = %.3f — still on the old flat scale", v.DesignResistance)
	}
	if v.Damper != 1.0 {
		t.Errorf("a freshly built box should start at its design damper position, got %.2f", v.Damper)
	}

	// And a building with far more boxes must not shrink each box's flow: the fan follows
	// the network. This is the half of the bug that made the office fixture look fine.
	var big strings.Builder
	big.WriteString(`{"buildingId":"bldg-big","floors":[{"height":2.8,"zones":[`)
	for i := 0; i < 40; i++ {
		if i > 0 {
			big.WriteString(",")
		}
		big.WriteString(`{"zoneId":"z`)
		big.WriteString(string(rune('a' + i%26)))
		big.WriteString(string(rune('0' + i/26)))
		big.WriteString(`","zoneType":"home-office","volume":60,"thermalProperties":{"setpoint":26,"deadband":2,"baseHeatLoad":300},"hvacMapping":{"vavId":"v`)
		big.WriteString(string(rune('a' + i%26)))
		big.WriteString(string(rune('0' + i/26)))
		big.WriteString(`"}}`)
	}
	big.WriteString(`]}]}`)

	e2 := engineFor(t, big.String())
	e2.doHardyCross()
	for id, vv := range e2.Vavs {
		if vv.Flow < want*0.8 || vv.Flow > want*1.25 {
			t.Fatalf("%s in a 40-zone building flows %.4f m³/s, want ≈%.4f — per-box flow "+
				"still depends on the zone count", id, vv.Flow, want)
		}
	}
}

// State can be stale for a second reason: it can describe THIS building under a model the
// engine no longer runs. Occupancy drives the zone temperatures the baselines score, the
// whole-building load the pre-cool trigger reads, the recorded megawatt series both
// forecasters consume, the range the plausibility check refuses a forecast against, and the
// peak the battery's nameplate is sized from. When a fix changed the modelled occupancy from
// a fixed random draw to a real diurnal schedule, every one of those files went on
// describing a house with twenty-eight phantom occupants in it — and unlike a wrong-building
// restore, the building id matched, so nothing caught it.

func withModelVersion(t *testing.T, raw []byte, ver int) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m["occupancyModelVersion"] = json.RawMessage(itoa(ver))
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

func itoa(v int) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestLoadHistoryFromAPreviousOccupancyModelIsDiscarded(t *testing.T) {
	a := engineFor(t, houseA)
	a.loadHist = []float64{1.5, 1.6, 1.7}
	saved, err := a.MarshalLoadHistory()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	stale := withModelVersion(t, saved, OccupancyModelVersion-1)
	a2 := engineFor(t, houseA)
	if err := a2.LoadLoadHistory(stale); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(a2.LoadHistory()); got != 0 {
		t.Errorf("restored %d samples recorded under a superseded occupancy model; the "+
			"plausibility check and the battery's nameplate would both be anchored to them", got)
	}

	// The current version still restores — the whole point of persisting it.
	a3 := engineFor(t, houseA)
	if err := a3.LoadLoadHistory(saved); err != nil {
		t.Fatalf("load current: %v", err)
	}
	if got := len(a3.LoadHistory()); got != 3 {
		t.Errorf("current model restored %d of 3 samples; a restart must not cost the context", got)
	}
}

func TestRoomModelsFromAPreviousOccupancyModelAreDiscarded(t *testing.T) {
	a := engineFor(t, houseA)
	// Enough moving samples that the room passes the fit's persistence threshold; the
	// drivers vary so the excitation gate accepts them.
	for i := 0; i < 12; i++ {
		a.dynamics.Observe([]RoomCondition{{
			Zone: "zone-office-lvl1", Temp: 26 + 0.4*float64(i%4),
			OutdoorC: 31 + 0.5*float64(i%3), FlowRatio: 0.3 + 0.05*float64(i%5),
			Occupancy: i % 4,
		}}, float64(i)*dynamicsSampleSimSecs)
	}
	saved, err := a.MarshalDynamics()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	stale := withModelVersion(t, saved, OccupancyModelVersion-1)
	b := engineFor(t, houseA)
	if err := b.LoadDynamics(stale); err != nil {
		t.Fatalf("load: %v", err)
	}
	// Occupancy is a regressor in the thermal fit; a fit made when it was a constant never
	// had that coefficient constrained by data.
	if id, learning := b.dynamics.Coverage(); id+learning > 0 {
		t.Errorf("restored %d rooms identified under a superseded occupancy model", id+learning)
	}

	// The current version still restores, or a restart throws away every hour of
	// identification the twin has done.
	c := engineFor(t, houseA)
	if err := c.LoadDynamics(saved); err != nil {
		t.Fatalf("load current: %v", err)
	}
	if id, learning := c.dynamics.Coverage(); id+learning == 0 {
		t.Error("current model restored no rooms; a restart would cost the identification")
	}
}

func TestBaselinesFromAPreviousOccupancyModelAreDiscarded(t *testing.T) {
	a := engineFor(t, houseA)
	now := time.Now()
	for i := 0; i < 40; i++ {
		a.baselines.Observe("zone-office-lvl1", "temp", 26+float64(i%3), now.Add(time.Duration(i)*time.Minute))
		a.baselines.Observe("GLOBAL", "buildingLoadMw", 0.6, now.Add(time.Duration(i)*time.Minute))
	}
	saved, err := a.MarshalBaselines()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	stale := withModelVersion(t, saved, OccupancyModelVersion-1)
	b := engineFor(t, houseA)
	if err := b.LoadBaselines(stale); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(b.baselines.Snapshot()); got != 0 {
		t.Errorf("restored %d baseline series learned under a superseded occupancy model; "+
			"what a zone's normal looks like changed with the occupancy that drives it", got)
	}

	// Current version still restores, including the per-zone series.
	c := engineFor(t, houseA)
	if err := c.LoadBaselines(saved); err != nil {
		t.Fatalf("load current: %v", err)
	}
	if got := len(c.baselines.Snapshot()); got == 0 {
		t.Error("current model restored nothing; a restart would cost every learned baseline")
	}
}

func TestUnversionedBaselinesAreDiscardedRatherThanPartlyTrusted(t *testing.T) {
	// The legacy on-disk form records neither a building nor a model version. It used to
	// be restored with only its whole-building buckets dropped, which left every per-zone
	// normal in place — learned under an occupancy this engine no longer drives.
	legacy := []byte(`{"zone-office-lvl1|temp":{"14":{"n":40,"mean":26.0,"m2":12.0}}}`)
	e := engineFor(t, houseA)
	if err := e.LoadBaselines(legacy); err != nil {
		t.Fatalf("load legacy: %v", err)
	}
	if got := len(e.baselines.Snapshot()); got != 0 {
		t.Errorf("restored %d unversioned baseline series; they cannot show which building "+
			"or which occupancy model they describe", got)
	}
}

// The third axis: state can describe the right building, under the right model, and still
// have been learned somewhere else. The fixture travels with the machine — a laptop running
// building-data.local.json at a demo keeps the same buildingId, so every other check passes
// while the engine folds a different building's ambient, occupancy and load into the house's
// learned normal.

func withSite(t *testing.T, raw []byte, site string) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b, _ := json.Marshal(site)
	m["site"] = b
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

// declareSite pins this process's own fingerprint for the duration of a test.
func declareSite(t *testing.T, name string) string {
	t.Helper()
	prev, had := os.LookupEnv("SITE_FINGERPRINT")
	os.Setenv("SITE_FINGERPRINT", name)
	t.Cleanup(func() {
		if had {
			os.Setenv("SITE_FINGERPRINT", prev)
		} else {
			os.Unsetenv("SITE_FINGERPRINT")
		}
	})
	return SiteFingerprint()
}

func TestLoadHistoryLearnedAtAnotherSiteIsDiscarded(t *testing.T) {
	here := declareSite(t, "the-house")
	away := declareSite(t, "a-conference-room")
	_ = away

	a := engineFor(t, houseA)
	a.loadHist = []float64{1.5, 1.6, 1.7}
	saved, err := a.MarshalLoadHistory()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Same building, same occupancy model, different network.
	elsewhere := withSite(t, saved, "a-different-network-entirely")
	b := engineFor(t, houseA)
	if err := b.LoadLoadHistory(elsewhere); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(b.LoadHistory()); got != 0 {
		t.Errorf("restored %d samples measured on another network; they are the range the "+
			"plausibility check refuses a forecast against and the peak the battery is "+
			"sized from", got)
	}

	// The same network still restores — the whole point of persisting it.
	sameNet := withSite(t, saved, here)
	declareSite(t, "the-house")
	c := engineFor(t, houseA)
	if err := c.LoadLoadHistory(sameNet); err != nil {
		t.Fatalf("load same site: %v", err)
	}
	if got := len(c.LoadHistory()); got != 3 {
		t.Errorf("same site restored %d of 3 samples; a restart at home must not cost the context", got)
	}
}

func TestStateWithNoRecordedSiteStillRestores(t *testing.T) {
	// State written before this check existed carries no site, and an engine with no
	// gateway to probe can offer none. Neither is grounds for destroying it: an unanswered
	// question is not evidence of a different building.
	declareSite(t, "the-house")

	a := engineFor(t, houseA)
	a.loadHist = []float64{1.5, 1.6, 1.7}
	saved, err := a.MarshalLoadHistory()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	untagged := withSite(t, saved, "")

	b := engineFor(t, houseA)
	if err := b.LoadLoadHistory(untagged); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(b.LoadHistory()); got != 3 {
		t.Errorf("discarded %d untagged samples; state that predates the site check must "+
			"still restore", 3-got)
	}
}

func TestBaselinesLearnedAtAnotherSiteAreDiscarded(t *testing.T) {
	declareSite(t, "the-house")

	a := engineFor(t, houseA)
	now := time.Now()
	for i := 0; i < 40; i++ {
		a.baselines.Observe("zone-office-lvl1", "temp", 26+float64(i%3), now.Add(time.Duration(i)*time.Minute))
	}
	saved, err := a.MarshalBaselines()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	elsewhere := withSite(t, saved, "a-different-network-entirely")
	b := engineFor(t, houseA)
	if err := b.LoadBaselines(elsewhere); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(b.baselines.Snapshot()); got != 0 {
		t.Errorf("restored %d baseline series learned on another network as this "+
			"building's own normal", got)
	}
}
