package main

import (
	"econ/simulation"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Challenge 1: Space Utilization Calculations & Boundary Conditions
func TestChallengeSpaceUtilizationBoundaries(t *testing.T) {
	// Case 1A: 0 occupants in occupiable zones
	engine0 := &simulation.Engine{
		Zones: map[string]*simulation.ZoneSim{
			"office-1": {
				Type:      "open-office", // 10 m2/person
				AreaM2:    100.0,         // capacity = 10
				Occupancy: 0,
			},
			"meeting-1": {
				Type:      "meeting-room", // 2.5 m2/person
				AreaM2:    25.0,          // capacity = 10
				Occupancy: 0,
			},
		},
	}
	space0 := computeSpaceUtilization(engine0)
	if space0.TotalBuildingCapacity != 20 {
		t.Errorf("[0-occupancy] expected capacity 20, got %d", space0.TotalBuildingCapacity)
	}
	if space0.TotalLiveOccupants != 0 {
		t.Errorf("[0-occupancy] expected 0 occupants, got %d", space0.TotalLiveOccupants)
	}
	if space0.OverallEfficiencyPercent != 0.0 {
		t.Errorf("[0-occupancy] expected 0.0%% efficiency, got %f", space0.OverallEfficiencyPercent)
	}
	for _, z := range space0.Zones {
		if z.EfficiencyPercent != 0.0 {
			t.Errorf("[0-occupancy] zone %s efficiency expected 0.0, got %f", z.ZoneId, z.EfficiencyPercent)
		}
	}

	// Case 1B: Full occupancy (100% capacity)
	engineFull := &simulation.Engine{
		Zones: map[string]*simulation.ZoneSim{
			"office-1": {
				Type:      "open-office", // capacity = 10
				AreaM2:    100.0,
				Occupancy: 10,
			},
			"meeting-1": {
				Type:      "meeting-room", // capacity = 10
				AreaM2:    25.0,
				Occupancy: 10,
			},
		},
	}
	spaceFull := computeSpaceUtilization(engineFull)
	if spaceFull.TotalBuildingCapacity != 20 {
		t.Errorf("[full-occupancy] expected capacity 20, got %d", spaceFull.TotalBuildingCapacity)
	}
	if spaceFull.TotalLiveOccupants != 20 {
		t.Errorf("[full-occupancy] expected 20 occupants, got %d", spaceFull.TotalLiveOccupants)
	}
	if spaceFull.OverallEfficiencyPercent != 100.0 {
		t.Errorf("[full-occupancy] expected 100.0%% efficiency, got %f", spaceFull.OverallEfficiencyPercent)
	}
	for _, z := range spaceFull.Zones {
		if z.EfficiencyPercent != 100.0 {
			t.Errorf("[full-occupancy] zone %s efficiency expected 100.0, got %f", z.ZoneId, z.EfficiencyPercent)
		}
	}

	// Case 1C: 200% over-capacity boundary condition
	engine200 := &simulation.Engine{
		Zones: map[string]*simulation.ZoneSim{
			"office-packed": {
				Type:      "open-office", // capacity = 10
				AreaM2:    100.0,
				Occupancy: 20, // 200%
			},
			"meeting-packed": {
				Type:      "meeting-room", // capacity = 10
				AreaM2:    25.0,
				Occupancy: 20, // 200%
			},
		},
	}
	space200 := computeSpaceUtilization(engine200)
	if space200.TotalBuildingCapacity != 20 {
		t.Errorf("[200%%-occupancy] expected capacity 20, got %d", space200.TotalBuildingCapacity)
	}
	if space200.TotalLiveOccupants != 40 {
		t.Errorf("[200%%-occupancy] expected 40 occupants, got %d", space200.TotalLiveOccupants)
	}
	if space200.OverallEfficiencyPercent != 200.0 {
		t.Errorf("[200%%-occupancy] expected 200.0%% efficiency, got %f", space200.OverallEfficiencyPercent)
	}
	for _, z := range space200.Zones {
		if z.EfficiencyPercent != 200.0 {
			t.Errorf("[200%%-occupancy] zone %s efficiency expected 200.0, got %f", z.ZoneId, z.EfficiencyPercent)
		}
	}

	// Case 1D: Nil engine and empty zones robustness
	spaceNil := computeSpaceUtilization(nil)
	if spaceNil.TotalBuildingCapacity != 0 || spaceNil.TotalLiveOccupants != 0 || len(spaceNil.Zones) != 0 {
		t.Errorf("[nil-engine] expected empty space utilization, got %+v", spaceNil)
	}
	spaceEmpty := computeSpaceUtilization(&simulation.Engine{Zones: map[string]*simulation.ZoneSim{}})
	if spaceEmpty.TotalBuildingCapacity != 0 || spaceEmpty.TotalLiveOccupants != 0 || len(spaceEmpty.Zones) != 0 {
		t.Errorf("[empty-zones] expected empty space utilization, got %+v", spaceEmpty)
	}
}

// Challenge 1 (cont): Exclusion of non-occupiable zones (corridor, plant-room, wet-core, etc.)
func TestChallengeNonOccupiableZonesExclusion(t *testing.T) {
	nonOccupiableTypes := []string{
		"corridor",
		"Corridor",
		" corridor ",
		"plant-room",
		"Plant-Room",
		" plant-room ",
		"wet-core",
		"Wet-Core",
		" wet-core ",
		"store",
		"Store",
		"comms-room",
		"mechanical",
		"server-room",
	}

	zones := make(map[string]*simulation.ZoneSim)
	// Add an occupiable office zone
	zones["zone-valid-office"] = &simulation.ZoneSim{
		Type:      "open-office",
		AreaM2:    50.0, // capacity = 5
		Occupancy: 3,
	}

	// Add each non-occupiable zone with non-zero area and non-zero phantom occupancy
	for i, tName := range nonOccupiableTypes {
		zoneId := fmt.Sprintf("non-occ-%d", i)
		zones[zoneId] = &simulation.ZoneSim{
			Type:      tName,
			AreaM2:    80.0, // large area
			Occupancy: 5,    // phantom occupancy reported by sensor glitch
		}
	}

	engine := &simulation.Engine{Zones: zones}
	space := computeSpaceUtilization(engine)

	// Verify ONLY the valid office is included in capacity and zones
	if space.TotalBuildingCapacity != 5 {
		t.Fatalf("[non-occupiable] expected building capacity 5, got %d (non-occupiable zones leaked into capacity!)", space.TotalBuildingCapacity)
	}
	if space.TotalLiveOccupants != 3 {
		t.Fatalf("[non-occupiable] expected live occupants 3, got %d (non-occupiable zone occupants leaked!)", space.TotalLiveOccupants)
	}
	if len(space.Zones) != 1 {
		t.Fatalf("[non-occupiable] expected exactly 1 zone in space utilization, got %d", len(space.Zones))
	}
	if space.Zones[0].ZoneId != "zone-valid-office" {
		t.Fatalf("[non-occupiable] expected zone-valid-office, got %s", space.Zones[0].ZoneId)
	}
	if space.OverallEfficiencyPercent != 60.0 {
		t.Fatalf("[non-occupiable] expected 60.0%% efficiency, got %f", space.OverallEfficiencyPercent)
	}
}

// Challenge 2: Predictive Maintenance Diagnostics
func TestChallengePredictiveMaintenanceDiagnostics(t *testing.T) {
	now := time.Now()

	// 2A: Power strip overload (> 2000W)
	t.Run("PowerStripOverloadBoundary", func(t *testing.T) {
		// Sub-case: exact threshold 2000.0W -> NO alert
		engineAtBoundary := &simulation.Engine{
			Zones: map[string]*simulation.ZoneSim{
				"zone-strip": {Type: "open-office", AreaM2: 50.0, HwStripW: 2000.0},
			},
		}
		tracker := newCarbonTracker(engineAtBoundary)
		snap := tracker.Snapshot(engineAtBoundary, now)
		for _, w := range snap.PredictiveMaintenance.Warnings {
			if w.Type == "over_capacity" && w.EquipmentId == "strip-zone-strip" {
				t.Fatalf("did not expect alert at exact rated 2000.0W, but got: %+v", w)
			}
		}

		// Sub-case: 2000.1W -> Alert triggered!
		engineOver := &simulation.Engine{
			Zones: map[string]*simulation.ZoneSim{
				"zone-strip": {Type: "open-office", AreaM2: 50.0, HwStripW: 2000.1},
			},
		}
		snapOver := tracker.Snapshot(engineOver, now.Add(time.Second))
		found := false
		for _, w := range snapOver.PredictiveMaintenance.Warnings {
			if w.Type == "over_capacity" && w.EquipmentId == "strip-zone-strip" {
				found = true
				if w.Threshold != 2000.0 || w.MetricValue != 2000.1 {
					t.Errorf("mismatch in alert details: %+v", w)
				}
				if w.Severity != "warning" {
					t.Errorf("expected severity 'warning', got %s", w.Severity)
				}
			}
		}
		if !found {
			t.Errorf("expected over_capacity alert at 2000.1W")
		}
	})

	// 2B: AC overload (> 3500W)
	t.Run("ACOverloadBoundary", func(t *testing.T) {
		// Sub-case: exact threshold 3500.0W -> NO alert
		engineAtBoundary := &simulation.Engine{
			Zones: map[string]*simulation.ZoneSim{
				"zone-ac": {Type: "open-office", AreaM2: 50.0, HwAcW: 3500.0},
			},
		}
		tracker := newCarbonTracker(engineAtBoundary)
		snap := tracker.Snapshot(engineAtBoundary, now)
		for _, w := range snap.PredictiveMaintenance.Warnings {
			if w.Type == "over_capacity" && w.EquipmentId == "ac-zone-ac" {
				t.Fatalf("did not expect alert at exact rated 3500.0W, but got: %+v", w)
			}
		}

		// Sub-case: 3500.1W -> Alert triggered!
		engineOver := &simulation.Engine{
			Zones: map[string]*simulation.ZoneSim{
				"zone-ac": {Type: "open-office", AreaM2: 50.0, HwAcW: 3500.1},
			},
		}
		snapOver := tracker.Snapshot(engineOver, now.Add(time.Second))
		found := false
		for _, w := range snapOver.PredictiveMaintenance.Warnings {
			if w.Type == "over_capacity" && w.EquipmentId == "ac-zone-ac" {
				found = true
				if w.Threshold != 3500.0 || w.MetricValue != 3500.1 {
					t.Errorf("mismatch in alert details: %+v", w)
				}
				if w.Severity != "warning" {
					t.Errorf("expected severity 'warning', got %s", w.Severity)
				}
			}
		}
		if !found {
			t.Errorf("expected over_capacity alert at 3500.1W")
		}
	})

	// 2C: Transient surge (> 1000W delta)
	t.Run("TransientSurgeBoundary", func(t *testing.T) {
		engine := &simulation.Engine{
			Zones: map[string]*simulation.ZoneSim{
				"zone-surge": {Type: "open-office", AreaM2: 50.0, HwStripW: 300.0},
			},
		}
		tracker := newCarbonTracker(engine)
		// Tick 1: establish baseline 300W
		_ = tracker.Snapshot(engine, now)

		// Step 2: increase to 1300.0W (delta = 1000.0W, exact boundary -> NO alert)
		engine.Zones["zone-surge"].HwStripW = 1300.0
		snapBoundary := tracker.Snapshot(engine, now.Add(time.Second))
		for _, w := range snapBoundary.PredictiveMaintenance.Warnings {
			if w.Type == "power_surge" && w.EquipmentId == "strip-zone-surge" {
				t.Fatalf("did not expect power_surge alert at exact 1000.0W delta, got: %+v", w)
			}
		}

		// Step 3: increase to 2300.1W (last was 1300.0W, delta = 1000.1W -> alert triggered!)
		engine.Zones["zone-surge"].HwStripW = 2300.1
		snapSurge := tracker.Snapshot(engine, now.Add(2*time.Second))
		foundSurge := false
		for _, w := range snapSurge.PredictiveMaintenance.Warnings {
			if w.Type == "power_surge" && w.EquipmentId == "strip-zone-surge" {
				foundSurge = true
				if w.Threshold != 1000.0 {
					t.Errorf("expected surge threshold 1000.0, got %f", w.Threshold)
				}
				// Delta is 2300.1 - 1300.0 = 1000.1
				if w.MetricValue < 1000.09 || w.MetricValue > 1000.11 {
					t.Errorf("expected delta ~1000.1, got %f", w.MetricValue)
				}
			}
		}
		if !foundSurge {
			t.Errorf("expected power_surge alert for +1000.1W delta")
		}

		// Step 4: large drop (2300.1W -> 100.0W) -> Negative delta, must NOT trigger surge alert
		engine.Zones["zone-surge"].HwStripW = 100.0
		snapDrop := tracker.Snapshot(engine, now.Add(3*time.Second))
		for _, w := range snapDrop.PredictiveMaintenance.Warnings {
			if w.Type == "power_surge" && w.EquipmentId == "strip-zone-surge" {
				t.Fatalf("negative power delta should not trigger surge alert, got: %+v", w)
			}
		}
	})

	// 2D: Cumulative runtime hours (> 2000h)
	t.Run("CumulativeRuntimeHoursBoundary", func(t *testing.T) {
		engine := &simulation.Engine{
			Zones: map[string]*simulation.ZoneSim{
				"zone-rt": {Type: "open-office", AreaM2: 50.0, HwStripW: 100.0},
			},
		}
		tracker := newCarbonTracker(engine)

		// Sub-case: runtime = 2000.0h exact boundary -> NO alert
		tracker.SetEquipmentRuntimeHours("strip-zone-rt", 2000.0)
		snapBoundary := tracker.Snapshot(engine, now)
		for _, w := range snapBoundary.PredictiveMaintenance.Warnings {
			if w.Type == "runtime_exceeded" && w.EquipmentId == "strip-zone-rt" {
				t.Fatalf("did not expect runtime_exceeded alert at exact 2000.0h, got: %+v", w)
			}
		}

		// Sub-case: runtime = 2000.1h -> Alert triggered!
		tracker.SetEquipmentRuntimeHours("strip-zone-rt", 2000.1)
		snapOver := tracker.Snapshot(engine, now.Add(time.Second))
		found := false
		for _, w := range snapOver.PredictiveMaintenance.Warnings {
			if w.Type == "runtime_exceeded" && w.EquipmentId == "strip-zone-rt" {
				found = true
				if w.Threshold != 2000.0 || w.MetricValue != 2000.1 {
					t.Errorf("mismatch in runtime alert details: %+v", w)
				}
				if w.Severity != "warning" {
					t.Errorf("expected severity 'warning', got %s", w.Severity)
				}
			}
		}
		if !found {
			t.Errorf("expected runtime_exceeded alert at 2000.1h")
		}

		// Sub-case: Verify runtime accumulation logic
		// Active equipment (> 5W) increments runtime by dtSec / 3600.
		// Standby equipment (<= 5W) does not increment.
		tracker.SetEquipmentRuntimeHours("strip-zone-rt", 10.0)
		tracker.lastTickAt = now
		// Running 100W for 1800s (0.5 hour)
		snapAccum := tracker.Snapshot(engine, now.Add(1800*time.Second))
		tracker.mu.Lock()
		accumHours := tracker.equipmentRuntimeHours["strip-zone-rt"]
		tracker.mu.Unlock()
		if accumHours < 10.49 || accumHours > 10.51 {
			t.Errorf("expected runtime ~10.5 hours after 0.5h run, got %f", accumHours)
		}
		_ = snapAccum

		// Idle equipment (4.0W <= 5.0W threshold) for 3600s
		engine.Zones["zone-rt"].HwStripW = 4.0
		tracker.Snapshot(engine, now.Add(5400*time.Second))
		tracker.mu.Lock()
		idleHours := tracker.equipmentRuntimeHours["strip-zone-rt"]
		tracker.mu.Unlock()
		if idleHours != accumHours {
			t.Errorf("idle equipment (4W <= 5W) should not increment runtime, was %f now %f", accumHours, idleHours)
		}
	})
}

// Challenge 3: /api/sustainability REST Endpoint Payload & Schema Compliance
func TestChallengeSustainabilityEndpointPayloadAndCORS(t *testing.T) {
	engine := &simulation.Engine{
		Zones: map[string]*simulation.ZoneSim{
			"zone-a": {
				Type:      "open-office",
				AreaM2:    100.0,
				Occupancy: 6,
				HwPlugW:   500.0,
				HwStripW:  2100.0, // over-capacity
				HwAcW:     1500.0,
			},
		},
	}
	tracker := newCarbonTracker(engine)
	tracker.SetCumulativeEmissions(62.5) // over budget (> 50 kg)
	tracker.SetCarbonBudget(50.0)

	handler := sustainabilityHandler(engine, tracker)

	// 3A: CORS Preflight OPTIONS Request
	t.Run("CORSPreflightOPTIONS", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/sustainability", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected OPTIONS status 204 No Content, got %d", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("expected Access-Control-Allow-Origin: *, got %s", rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Access-Control-Allow-Methods") != "GET, POST, OPTIONS" {
			t.Errorf("expected Access-Control-Allow-Methods 'GET, POST, OPTIONS', got %s", rec.Header().Get("Access-Control-Allow-Methods"))
		}
		if rec.Header().Get("Access-Control-Allow-Headers") != "Content-Type, X-Admin-Token" {
			t.Errorf("expected Access-Control-Allow-Headers 'Content-Type, X-Admin-Token', got %s", rec.Header().Get("Access-Control-Allow-Headers"))
		}
	})

	// 3B: HTTP Method Not Allowed for POST, PUT, DELETE
	t.Run("MethodNotAllowed", func(t *testing.T) {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			req := httptest.NewRequest(method, "/api/sustainability", nil)
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected method %s to return 405 Method Not Allowed, got %d", method, rec.Code)
			}
		}
	})

	// 3C: GET 200 OK and Strict Schema Verification
	t.Run("GETSuccessAndSchemaValidation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sustainability", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected GET status 200 OK, got %d", rec.Code)
		}
		if rec.Header().Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("expected Access-Control-Allow-Origin: *, got %s", rec.Header().Get("Access-Control-Allow-Origin"))
		}

		rawBody := rec.Body.Bytes()

		// Verify valid JSON by unmarshaling to raw map
		var rawMap map[string]interface{}
		if err := json.Unmarshal(rawBody, &rawMap); err != nil {
			t.Fatalf("payload is not valid JSON: %v", err)
		}

		// Verify top-level timestamp
		if _, ok := rawMap["timestamp"]; !ok {
			t.Errorf("missing top-level 'timestamp' key")
		}

		// Verify Section 1: carbonAccounting
		sec1, ok := rawMap["carbonAccounting"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing or invalid 'carbonAccounting' section")
		}
		for _, field := range []string{
			"gridEmissionFactorKgPerKwh",
			"instantaneousPowerW",
			"instantaneousEmissionRateKgPerHour",
			"cumulativeEmissionsKgCO2e",
			"breakdown",
		} {
			if _, ok := sec1[field]; !ok {
				t.Errorf("carbonAccounting missing field: %s", field)
			}
		}
		breakdown, ok := sec1["breakdown"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing or invalid carbonAccounting.breakdown")
		}
		for _, bField := range []string{"plugW", "stripW", "acW"} {
			if _, ok := breakdown[bField]; !ok {
				t.Errorf("carbonAccounting.breakdown missing field: %s", bField)
			}
		}

		// Verify Section 2: spaceUtilization
		sec2, ok := rawMap["spaceUtilization"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing or invalid 'spaceUtilization' section")
		}
		for _, field := range []string{
			"totalLiveOccupants",
			"totalBuildingCapacity",
			"overallEfficiencyPercent",
			"zones",
		} {
			if _, ok := sec2[field]; !ok {
				t.Errorf("spaceUtilization missing field: %s", field)
			}
		}

		// Verify Section 3: predictiveMaintenance
		sec3, ok := rawMap["predictiveMaintenance"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing or invalid 'predictiveMaintenance' section")
		}
		for _, field := range []string{"activeAlertsCount", "warnings"} {
			if _, ok := sec3[field]; !ok {
				t.Errorf("predictiveMaintenance missing field: %s", field)
			}
		}

		// Verify Section 4: carbonCreditRecommendations
		sec4, ok := rawMap["carbonCreditRecommendations"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing or invalid 'carbonCreditRecommendations' section")
		}
		for _, field := range []string{
			"carbonBudgetKgCO2e",
			"currentEmissionsKgCO2e",
			"overBudget",
			"deficitKgCO2e",
			"creditsNeededMetricTons",
			"wholeCertificatesNeeded",
			"marketQuote",
			"estimatedCostUSD",
			"recommendation",
		} {
			if _, ok := sec4[field]; !ok {
				t.Errorf("carbonCreditRecommendations missing field: %s", field)
			}
		}
		quote, ok := sec4["marketQuote"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing or invalid carbonCreditRecommendations.marketQuote")
		}
		for _, qField := range []string{"source", "spotPricePerMetricTonUSD", "currency", "isLive", "fetchedAt"} {
			if _, ok := quote[qField]; !ok {
				t.Errorf("marketQuote missing field: %s", qField)
			}
		}

		// Also unmarshal into strong struct
		var payload SustainabilityPayload
		if err := json.Unmarshal(rawBody, &payload); err != nil {
			t.Fatalf("failed strong struct unmarshal: %v", err)
		}
		if payload.CarbonCreditRecommendations.OverBudget != true {
			t.Errorf("expected overBudget == true for 62.5 kg against 50.0 kg budget")
		}
		if payload.PredictiveMaintenance.ActiveAlertsCount < 1 {
			t.Errorf("expected at least 1 alert for 2100W strip, got %d", payload.PredictiveMaintenance.ActiveAlertsCount)
		}
		if payload.SpaceUtilization.TotalLiveOccupants != 6 {
			t.Errorf("expected 6 occupants, got %d", payload.SpaceUtilization.TotalLiveOccupants)
		}
	})
}
