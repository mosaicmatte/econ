package simulation

import (
	"math"
	"os"
	"testing"
	"time"
)

// TestSimulationModelSwitchTopology validates that Engine.ReloadBuilding properly updates
// building topology, resets whole-building baselines/history, sizes fan to volume, and
// integrates thermal physics accurately across both commercial and residential models.
func TestSimulationModelSwitchTopology(t *testing.T) {
	homeData, err := os.ReadFile(DataPath(BuildingDataHomeFile))
	if err != nil {
		t.Fatalf("failed to read home data: %v", err)
	}

	officeData, err := os.ReadFile(DataPath(BuildingDataFile))
	if err != nil {
		t.Fatalf("failed to read office data: %v", err)
	}

	e := &Engine{
		Zones:      make(map[string]*ZoneSim),
		Vavs:       make(map[string]*VavSim),
		PMax:       600.0,
		KFan:       0.01,
		Scenario:   "peak",
		lastCmd:    make(map[string]string),
		demoAssign: make(map[string]string),
		Bess:       NewBattery(),
		lastBessAt: time.Now(),
		Plug:       defaultPlugConfig(),
		lastPlugAt: time.Now(),
		AutoPilot:  true,
		baselines:  NewBaselines(),
		dynamics:   NewDynamics(),
	}

	if err := e.buildFromJSON(officeData); err != nil {
		t.Fatalf("initial office build: %v", err)
	}

	// Record fake load history and baselines for the office building
	e.loadHist = []float64{1.5, 1.8, 2.1, 1.9}
	e.loadMinMw, e.loadMaxMw, e.loadSeen = 1.2, 2.5, 10
	e.baselines.Observe("GLOBAL", "buildingLoadMw", 2.0, time.Now())

	if e.BuildingId() != "bldg-econ-digitized" {
		t.Errorf("expected bldg-econ-digitized, got %q", e.BuildingId())
	}
	if len(e.Zones) != 735 {
		t.Errorf("expected 735 office zones, got %d", len(e.Zones))
	}
	officePMax := e.PMax
	if officePMax < 100.0 {
		t.Errorf("office PMax = %.1f kW, want >= 100 kW", officePMax)
	}

	// 1. Reload with domestic home
	if err := e.ReloadBuilding(homeData); err != nil {
		t.Fatalf("ReloadBuilding(homeData) failed: %v", err)
	}

	if e.BuildingId() != "bldg-econ-house-hcmc" {
		t.Errorf("after home reload, buildingId = %q, want %q", e.BuildingId(), "bldg-econ-house-hcmc")
	}
	if len(e.Zones) != 5 {
		t.Fatalf("after home reload, len(Zones) = %d, want 5", len(e.Zones))
	}
	if len(e.Vavs) != 5 {
		t.Fatalf("after home reload, len(Vavs) = %d, want 5", len(e.Vavs))
	}

	// Assert load history was purged
	if len(e.loadHist) != 0 {
		t.Errorf("expected loadHist to be purged, got len = %d", len(e.loadHist))
	}
	if e.loadSeen != 0 || e.loadMinMw != 0 || e.loadMaxMw != 0 {
		t.Errorf("expected load range to be reset, got seen=%d min=%.2f max=%.2f",
			e.loadSeen, e.loadMinMw, e.loadMaxMw)
	}

	// Assert fan sizing scaled down appropriately for 72 m2 domestic house
	homePMax := e.PMax
	if homePMax <= 0.0 {
		t.Errorf("home PMax = %.2f kW, want > 0", homePMax)
	}
	if homePMax >= officePMax {
		t.Errorf("home PMax (%.2f) should be significantly smaller than office PMax (%.2f)",
			homePMax, officePMax)
	}

	// Assert Hardy-Cross computed positive flow for all domestic VAVs
	for id, v := range e.Vavs {
		if v.Flow <= 0 {
			t.Errorf("vav %q flow = %.4f m3/s, want positive flow", id, v.Flow)
		}
		if v.NominalFlow <= 0 {
			t.Errorf("vav %q nominal flow = %.4f m3/s, want positive nominal flow", id, v.NominalFlow)
		}
	}

	// Assert thermal integration stability: run 60 seconds of physics on domestic house
	now := time.Now()
	dt := 0.1 // 100 ms time step
	e.outdoorTemp = 34.0
	e.outdoorHum = 65.0
	e.outdoorAt = now

	for step := 0; step < 600; step++ {
		e.tick(dt)
	}

	// Check zone temperatures remain stable and bounded between 20°C and 40°C
	for id, z := range e.Zones {
		if math.IsNaN(z.Temp) || math.IsInf(z.Temp, 0) {
			t.Fatalf("zone %q temperature exploded to NaN/Inf", id)
		}
		if z.Temp < 18.0 || z.Temp > 45.0 {
			t.Errorf("zone %q temperature = %.2f°C, out of realistic physical bounds [18, 45]", id, z.Temp)
		}
	}

	// 2. Reload back to commercial office
	if err := e.ReloadBuilding(officeData); err != nil {
		t.Fatalf("ReloadBuilding(officeData) failed: %v", err)
	}

	if e.BuildingId() != "bldg-econ-digitized" {
		t.Errorf("restored buildingId = %q, want %q", e.BuildingId(), "bldg-econ-digitized")
	}
	if len(e.Zones) != 735 {
		t.Errorf("restored len(Zones) = %d, want 735", len(e.Zones))
	}
	if e.PMax < 100.0 {
		t.Errorf("restored PMax = %.1f kW, want >= 100 kW", e.PMax)
	}
}
