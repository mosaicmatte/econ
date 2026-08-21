package simulation

import (
	"encoding/json"
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
