package main

import (
	"bytes"
	"econ/simulation"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// setupTestEngineWithZone creates a test engine instance with an isolated office zone.
func setupTestEngineWithZone(zoneId string) (*simulation.Engine, *simulation.ZoneSim) {
	e := simulation.NewEngine()
	e.Zones = make(map[string]*simulation.ZoneSim)
	z := &simulation.ZoneSim{
		Type:         "office",
		Temp:         24.0,
		BaseSetpoint: 24.0,
		Setpoint:     24.0,
		LightsOn:     true,
		MqttTopic:    zoneId,
		AreaM2:       50.0,
		Co2Sim:       400.0,
	}
	e.Zones[zoneId] = z
	return e, z
}

// TestAnomalousTelemetry_TurnOffAc verifies that anomalous telemetry triggers expected
// recommendations: vacant room with active AC -> turn_off_ac, high temp -> cool, high CO2 -> purge.
func TestAnomalousTelemetry_TurnOffAc(t *testing.T) {
	engine, z := setupTestEngineWithZone("zone-office-a")

	// 1. Inject anomalous telemetry for vacant room with AC active:
	// Occupancy = 0, PirState = false, IrState = "COOL_22" (not OFF)
	occZero := 0
	pirFalse := false
	irActive := "COOL_22"
	engine.IngestTelemetry("zone-office-a", "zone-office-a", simulation.Measurement{
		Occupancy: &occZero,
		PirState:  &pirFalse,
		IrState:   &irActive,
	})

	if z.Occupancy != 0 {
		t.Fatalf("expected z.Occupancy == 0, got %d", z.Occupancy)
	}
	if z.PirState == nil || *z.PirState != false {
		t.Fatalf("expected z.PirState == false, got %v", z.PirState)
	}
	if z.IrState == nil || *z.IrState != "COOL_22" {
		t.Fatalf("expected z.IrState == 'COOL_22', got %v", z.IrState)
	}

	// Verify engine.Recommendations(8) produces turn_off_ac
	rep := engine.Recommendations(8)
	var foundTurnOffAc bool
	for _, rec := range rep.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
			foundTurnOffAc = true
			if rec.Metric != "occupancy" {
				t.Errorf("expected Metric == 'occupancy', got %q", rec.Metric)
			}
			if rec.Severity == "" {
				t.Errorf("expected non-empty Severity, got %q", rec.Severity)
			}
			if rec.Title == "" {
				t.Errorf("expected non-empty Title")
			}
		}
	}
	if !foundTurnOffAc {
		t.Fatalf("expected turn_off_ac recommendation from engine.Recommendations(8), got %+v", rep.Recommendations)
	}

	// Verify GET /api/recommendations returns the turn_off_ac recommendation
	recHandler := recommendationsHandler(engine)
	req := httptest.NewRequest(http.MethodGet, "/api/recommendations", nil)
	rec := httptest.NewRecorder()
	recHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from recommendationsHandler, got %d", rec.Code)
	}
	var httpReport simulation.RecommendationReport
	if err := json.NewDecoder(rec.Body).Decode(&httpReport); err != nil {
		t.Fatalf("failed to decode recommendations json: %v", err)
	}
	var httpFoundTurnOffAc bool
	for _, r := range httpReport.Recommendations {
		if r.Zone == "zone-office-a" && r.Action == "turn_off_ac" {
			httpFoundTurnOffAc = true
			if r.Metric != "occupancy" {
				t.Errorf("expected http Metric == 'occupancy', got %q", r.Metric)
			}
			if r.Severity == "" {
				t.Errorf("expected non-empty http Severity")
			}
		}
	}
	if !httpFoundTurnOffAc {
		t.Fatalf("expected turn_off_ac recommendation in GET /api/recommendations, got %+v", httpReport.Recommendations)
	}

	// 2. Test High Temperature Anomaly -> Action == "cool"
	// Train the baseline so the temperature bucket matures
	now := time.Now()
	for i := 0; i < 35; i++ {
		engine.ObserveBaseline("zone-office-a", "temp", 22.0, now)
	}
	tempHigh := 32.0
	tempReal := true
	engine.IngestTelemetry("zone-office-a", "zone-office-a", simulation.Measurement{
		Temp:     &tempHigh,
		TempReal: tempReal,
	})
	z.Temp = 32.0
	z.Setpoint = 22.0

	repTemp := engine.Recommendations(8)
	var foundCool bool
	for _, rec := range repTemp.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "cool" {
			foundCool = true
			if rec.Metric != "temp" {
				t.Errorf("expected Metric == 'temp', got %q", rec.Metric)
			}
			if rec.Severity == "" {
				t.Errorf("expected non-empty Severity for cool recommendation")
			}
		}
	}
	if !foundCool {
		t.Fatalf("expected cool recommendation for high temperature, got %+v", repTemp.Recommendations)
	}

	// 3. Test High CO2 Anomaly -> Action == "purge"
	co2High := 1400.0 // > 1000 ppm ASHRAE threshold
	engine.IngestTelemetry("zone-office-a", "zone-office-a", simulation.Measurement{
		Co2: &co2High,
	})

	repCo2 := engine.Recommendations(8)
	var foundPurge bool
	for _, rec := range repCo2.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "purge" {
			foundPurge = true
			if rec.Metric != "co2" {
				t.Errorf("expected Metric == 'co2', got %q", rec.Metric)
			}
			if rec.Severity == "" {
				t.Errorf("expected non-empty Severity for purge recommendation")
			}
		}
	}
	if !foundPurge {
		t.Fatalf("expected purge recommendation for high CO2, got %+v", repCo2.Recommendations)
	}
}

// TestCommandHandler_ManualOverrideRouting verifies /api/command endpoint routing,
// 15-minute latching, zone state updates, autonomous actuation gating, and error rejections.
func TestCommandHandler_ManualOverrideRouting(t *testing.T) {
	engine, z := setupTestEngineWithZone("zone-office-a")
	var lastPublishedTopic, lastPublishedPayload string
	engine.Publish = func(topic, payload string) {
		lastPublishedTopic = topic
		lastPublishedPayload = payload
	}

	handler := commandHandler(engine)

	// 1. Accepts POST {"zone": "zone-office-a", "action": "turn_off_ac"}
	t.Run("POST with action field", func(t *testing.T) {
		body := `{"zone": "zone-office-a", "action": "turn_off_ac"}`
		req := httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify 15-minute manual override latch is set on z.OverrideUntil
		remainingLatch := time.Until(z.OverrideUntil)
		if remainingLatch < 14*time.Minute || remainingLatch > 15*time.Minute+5*time.Second {
			t.Fatalf("expected OverrideUntil to be ~15 minutes in future, got remaining duration: %v", remainingLatch)
		}

		// Verify zone state is updated (*z.IrState == "OFF")
		if z.IrState == nil || *z.IrState != "OFF" {
			t.Fatalf("expected *z.IrState == 'OFF', got %v", z.IrState)
		}

		// Verify MQTT publication
		if lastPublishedPayload != "HVAC_SET:OFF" {
			t.Errorf("expected published payload 'HVAC_SET:OFF', got %q", lastPublishedPayload)
		}
		if lastPublishedTopic != "econ/commands/zone-office-a" {
			t.Errorf("expected published topic 'econ/commands/zone-office-a', got %q", lastPublishedTopic)
		}

		// Verify autonomous engine.actuate() respects the manual override latch
		engine.SetAutoPilot(true)
		z.Occupancy = 0
		z.Setpoint = 19.5 // Human operator manual veto setpoint

		// Actuate should NOT modify z.Setpoint to setback because latch is active
		engine.Actuate()
		if z.Setpoint != 19.5 {
			t.Fatalf("autonomous actuate() stomped manual override setpoint: got %.1f, expected 19.5", z.Setpoint)
		}
		if z.IrState == nil || *z.IrState != "OFF" {
			t.Fatalf("autonomous actuate() stomped manual IrState: got %v, expected 'OFF'", z.IrState)
		}

		// When latch expires, autonomous actuate() CAN reassert control
		z.OverrideUntil = time.Now().Add(-1 * time.Minute)
		engine.Actuate()
		// Under AutoPilot and vacancy, unlatched zone receives autonomous setback (BaseSetpoint + 2.0 = 26.0)
		if z.Setpoint == 19.5 {
			t.Fatalf("expected autonomous actuate() to reassert control after latch expiration, but Setpoint remained 19.5")
		}
	})

	// 2. Accepts POST {"zone": "zone-office-a", "command": "turn_off_ac"}
	t.Run("POST with command field", func(t *testing.T) {
		// Reset state
		acOn := "COOL_22"
		z.IrState = &acOn
		z.OverrideUntil = time.Time{}
		lastPublishedPayload = ""

		body := `{"zone": "zone-office-a", "command": "turn_off_ac"}`
		req := httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		remainingLatch := time.Until(z.OverrideUntil)
		if remainingLatch < 14*time.Minute || remainingLatch > 15*time.Minute+5*time.Second {
			t.Fatalf("expected OverrideUntil to be ~15 minutes in future, got remaining duration: %v", remainingLatch)
		}
		if z.IrState == nil || *z.IrState != "OFF" {
			t.Fatalf("expected *z.IrState == 'OFF', got %v", z.IrState)
		}
		if lastPublishedPayload != "HVAC_SET:OFF" {
			t.Errorf("expected published payload 'HVAC_SET:OFF', got %q", lastPublishedPayload)
		}
	})

	// 3. Rejection of GET (405 Method Not Allowed)
	t.Run("GET method rejected with 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/command", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
		}
	})

	// 4. Rejection of malformed JSON (400 Bad Request)
	t.Run("Malformed JSON rejected with 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewBufferString("{bad-json"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	// 5. Rejection of empty fields (400 Bad Request)
	testEmptyCases := []struct {
		name string
		body string
	}{
		{"empty zone", `{"zone": "", "command": "turn_off_ac"}`},
		{"empty command and action", `{"zone": "zone-office-a", "command": "", "action": ""}`},
		{"empty json object", `{}`},
		{"whitespace zone", `{"zone": "   ", "command": "turn_off_ac"}`},
		{"whitespace command", `{"zone": "zone-office-a", "command": "   ", "action": "   "}`},
	}

	for _, tc := range testEmptyCases {
		t.Run("Validation: "+tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 Bad Request for %s, got %d", tc.name, rec.Code)
			}
		})
	}
}
