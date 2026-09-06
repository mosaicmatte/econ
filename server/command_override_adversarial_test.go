package main

import (
	"bytes"
	"econ/simulation"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// setupAdversarialTestEngine initializes an isolated test Engine with a test office zone.
func setupAdversarialTestEngine(zoneId string) (*simulation.Engine, *simulation.ZoneSim) {
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
		Live:         true,
	}
	e.Zones[zoneId] = z
	return e, z
}

// 1. Concurrency & Race Conditions:
// Concurrently dispatch 50+ manual override requests to /api/command while running
// engine.Actuate() and engine.IngestTelemetry() concurrently.
// Verified via go test -race.
func TestAdversarial_ConcurrencyRace(t *testing.T) {
	engine, z := setupAdversarialTestEngine("zone-office-a")
	handler := commandHandler(engine)

	var wg sync.WaitGroup
	var successfulOverrides atomic.Int64
	var telemetrySamples atomic.Int64
	var actuateCycles atomic.Int64

	numOverrides := 100
	concurrency := 10

	// Channel to throttle goroutines
	sem := make(chan struct{}, concurrency)

	// Worker Group A: 100 concurrent manual override HTTP POST requests to /api/command
	for i := 0; i < numOverrides; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			action := "turn_off_ac"
			if idx%2 == 0 {
				action = "cool"
			}
			body := fmt.Sprintf(`{"zone": "zone-office-a", "action": "%s"}`, action)
			req := httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				successfulOverrides.Add(1)
			}
		}(i)
	}

	// Worker Group B: Concurrent background engine.Actuate() executions
	stopActuate := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopActuate:
				return
			case <-ticker.C:
				engine.Actuate()
				actuateCycles.Add(1)
			}
		}
	}()

	// Worker Group C: Concurrent background engine.IngestTelemetry() executions
	stopTelemetry := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(3 * time.Millisecond)
		defer ticker.Stop()
		iteration := 0
		for {
			select {
			case <-stopTelemetry:
				return
			case <-ticker.C:
				iteration++
				occ := iteration % 5
				temp := 22.0 + float64(iteration%10)*0.5
				co2 := 400.0 + float64(iteration%100)*10.0
				ir := "COOL_22"
				if iteration%2 == 0 {
					ir = "OFF"
				}
				pir := (occ > 0)
				engine.IngestTelemetry("zone-office-a", "zone-office-a", simulation.Measurement{
					Occupancy: &occ,
					Temp:      &temp,
					TempReal:  true,
					Co2:       &co2,
					IrState:   &ir,
					PirState:  &pir,
				})
				telemetrySamples.Add(1)
			}
		}
	}()

	// Worker Group D: Concurrent recommendation generation
	stopRecs := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopRecs:
				return
			case <-ticker.C:
				_ = engine.Recommendations(5)
			}
		}
	}()

	// Wait for all 100 override requests to complete
	time.Sleep(100 * time.Millisecond)
	close(stopActuate)
	close(stopTelemetry)
	close(stopRecs)
	wg.Wait()

	t.Logf("Adversarial Concurrency Stress Results: overrides=%d/%d, actuate_cycles=%d, telemetry_samples=%d",
		successfulOverrides.Load(), numOverrides, actuateCycles.Load(), telemetrySamples.Load())

	if successfulOverrides.Load() != int64(numOverrides) {
		t.Fatalf("expected all %d override requests to succeed (200 OK), got %d", numOverrides, successfulOverrides.Load())
	}
	if actuateCycles.Load() == 0 {
		t.Errorf("expected > 0 actuate cycles, got %d", actuateCycles.Load())
	}
	if telemetrySamples.Load() == 0 {
		t.Errorf("expected > 0 telemetry samples, got %d", telemetrySamples.Load())
	}
	if z.Setpoint <= 0 {
		t.Errorf("invalid zone setpoint state: %.2f", z.Setpoint)
	}
}

// 2. Override Latch Boundary Invariants:
// - Immediately after override (t = 0m): manual command holds, autonomous setback blocked.
// - At t = 14m59s: manual command still holds, autonomous setback still blocked.
// - At t = 15m01s: manual latch has expired, autonomous setback successfully resumes.
func TestAdversarial_OverrideLatchBoundaryInvariants(t *testing.T) {
	engine, z := setupAdversarialTestEngine("zone-office-a")
	handler := commandHandler(engine)

	// Enable AutoPilot so autonomous setback would normally trigger
	engine.SetAutoPilot(true)

	// Configure zone for autonomous setback (unoccupied, vacant ticks past threshold)
	z.Occupancy = 0
	z.VacantTicks = 100
	z.BaseSetpoint = 24.0
	z.Setpoint = 24.0
	acCool := "COOL_22"
	z.IrState = &acCool

	// Dispatch manual override: turn_off_ac
	body := `{"zone": "zone-office-a", "action": "turn_off_ac"}`
	req := httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("command failed with status %d: %s", rec.Code, rec.Body.String())
	}

	// Boundary 1: Immediately after override (t = 0m, 15m00s remaining)
	t.Run("Boundary t = 0m (immediate)", func(t *testing.T) {
		remaining := time.Until(z.OverrideUntil)
		if remaining < 14*time.Minute+55*time.Second {
			t.Fatalf("expected latch to be ~15m, got %v", remaining)
		}

		// Operator manually set setpoint to 21.0
		z.Setpoint = 21.0
		if z.IrState == nil || *z.IrState != "OFF" {
			t.Fatalf("expected *z.IrState == 'OFF', got %v", z.IrState)
		}

		// Run autonomous actuate()
		engine.Actuate()

		// Invariant: manual command holds, autonomous setback is blocked
		if z.Setpoint != 21.0 {
			t.Fatalf("autonomous setback stomped manual setpoint at t=0m: got %.1f, expected 21.0", z.Setpoint)
		}
		if z.IrState == nil || *z.IrState != "OFF" {
			t.Fatalf("manual IrState stomped at t=0m: got %v, expected 'OFF'", z.IrState)
		}
	})

	// Boundary 2: At t = 14m59s (1 second remaining on latch)
	t.Run("Boundary t = 14m59s (1s before expiry)", func(t *testing.T) {
		// Simulate 14m59s elapsed: exactly 1s remaining on latch
		z.OverrideUntil = time.Now().Add(1 * time.Second)
		z.Setpoint = 21.0
		off := "OFF"
		z.IrState = &off

		// Run autonomous actuate()
		engine.Actuate()

		// Invariant: manual command holds, autonomous setback still blocked
		if z.Setpoint != 21.0 {
			t.Fatalf("autonomous setback stomped manual setpoint at t=14m59s: got %.1f, expected 21.0", z.Setpoint)
		}
		if z.IrState == nil || *z.IrState != "OFF" {
			t.Fatalf("manual IrState stomped at t=14m59s: got %v, expected 'OFF'", z.IrState)
		}
	})

	// Boundary 3: At t = 15m01s (1 second past latch expiry)
	t.Run("Boundary t = 15m01s (1s after expiry)", func(t *testing.T) {
		// Simulate 15m01s elapsed: latch expired by 1 second
		z.OverrideUntil = time.Now().Add(-1 * time.Second)
		z.Setpoint = 21.0 // Prior manual veto setpoint
		z.Occupancy = 0
		z.VacantTicks = 100 // Fully vacant

		// Run autonomous actuate()
		engine.Actuate()

		// Invariant: latch has expired, autonomous setback successfully resumes
		// BaseSetpoint (24.0) + setback delta (2.0) = 26.0
		if z.Setpoint == 21.0 {
			t.Fatalf("autonomous setback failed to resume at t=15m01s: Setpoint remained 21.0")
		}
		if z.Setpoint < 25.0 {
			t.Fatalf("expected setback setpoint >= 25.0°C, got %.1f°C", z.Setpoint)
		}
		if z.LightsOn {
			t.Fatalf("expected autonomous setback to turn lights off for vacant zone")
		}
	})
}

// 3. Anomaly Telemetry Disambiguation:
// - Inject normal telemetry (occupied room with AC on): assert turn_off_ac is NOT recommended.
// - Inject anomalous telemetry (unoccupied room with AC on): assert turn_off_ac IS recommended.
// - Inject normal temp / normal CO2: assert no cool or purge.
// - Inject anomalous spikes: assert appropriate actions triggered.
func TestAdversarial_AnomalyTelemetryDisambiguation(t *testing.T) {
	engine, z := setupAdversarialTestEngine("zone-office-a")

	// Case 3.1: Normal telemetry (occupied room with AC on)
	t.Run("Normal: occupied room with AC on", func(t *testing.T) {
		occ := 2
		pir := true
		ir := "COOL_22"
		temp := 24.0
		co2 := 600.0

		engine.IngestTelemetry("zone-office-a", "zone-office-a", simulation.Measurement{
			Occupancy: &occ,
			PirState:  &pir,
			IrState:   &ir,
			Temp:      &temp,
			TempReal:  true,
			Co2:       &co2,
		})

		rep := engine.Recommendations(10)
		for _, rec := range rep.Recommendations {
			if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
				t.Fatalf("FALSE POSITIVE: turn_off_ac recommended for occupied room: %+v", rec)
			}
		}
	})

	// Case 3.2: Anomalous telemetry (unoccupied room with AC on)
	t.Run("Anomalous: unoccupied room with AC on", func(t *testing.T) {
		occ := 0
		pir := false
		ir := "COOL_22"

		engine.IngestTelemetry("zone-office-a", "zone-office-a", simulation.Measurement{
			Occupancy: &occ,
			PirState:  &pir,
			IrState:   &ir,
		})

		rep := engine.Recommendations(10)
		var found bool
		for _, rec := range rep.Recommendations {
			if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
				found = true
				if rec.Metric != "occupancy" {
					t.Errorf("expected Metric == 'occupancy', got %q", rec.Metric)
				}
				if rec.Severity != "info" {
					t.Errorf("expected Severity == 'info', got %q", rec.Severity)
				}
			}
		}
		if !found {
			t.Fatalf("FALSE NEGATIVE: turn_off_ac NOT recommended for vacant room with AC on")
		}
	})

	// Case 3.3: Normal temp / normal CO2 -> assert NO cool or purge
	t.Run("Normal temp and normal CO2", func(t *testing.T) {
		temp := 24.0
		co2 := 550.0 // Well below 1000 ppm ASHRAE standard
		engine.IngestTelemetry("zone-office-a", "zone-office-a", simulation.Measurement{
			Temp:     &temp,
			TempReal: true,
			Co2:      &co2,
		})
		z.Temp = 24.0
		z.Setpoint = 24.0

		rep := engine.Recommendations(10)
		for _, rec := range rep.Recommendations {
			if rec.Zone == "zone-office-a" {
				if rec.Action == "cool" {
					t.Fatalf("FALSE POSITIVE: cool recommended for normal temp: %+v", rec)
				}
				if rec.Action == "purge" {
					t.Fatalf("FALSE POSITIVE: purge recommended for normal CO2: %+v", rec)
				}
			}
		}
	})

	// Case 3.4: Anomalous temperature spike -> assert cool triggered
	t.Run("Anomalous temperature spike -> cool", func(t *testing.T) {
		// Train baseline so bucket is mature
		now := time.Now()
		for i := 0; i < 35; i++ {
			engine.ObserveBaseline("zone-office-a", "temp", 22.0, now)
		}
		tempSpike := 33.0
		engine.IngestTelemetry("zone-office-a", "zone-office-a", simulation.Measurement{
			Temp:     &tempSpike,
			TempReal: true,
		})
		z.Temp = 33.0
		z.Setpoint = 22.0

		rep := engine.Recommendations(10)
		var foundCool bool
		for _, rec := range rep.Recommendations {
			if rec.Zone == "zone-office-a" && rec.Action == "cool" {
				foundCool = true
				if rec.Metric != "temp" {
					t.Errorf("expected Metric == 'temp', got %q", rec.Metric)
				}
				if rec.Severity != "critical" && rec.Severity != "warning" {
					t.Errorf("expected critical/warning Severity, got %q", rec.Severity)
				}
			}
		}
		if !foundCool {
			t.Fatalf("FALSE NEGATIVE: cool NOT recommended for thermal spike")
		}
	})

	// Case 3.5: Anomalous CO2 spike -> assert purge triggered
	t.Run("Anomalous CO2 spike -> purge", func(t *testing.T) {
		co2Spike := 1450.0 // > 1000 ppm
		engine.IngestTelemetry("zone-office-a", "zone-office-a", simulation.Measurement{
			Co2: &co2Spike,
		})

		rep := engine.Recommendations(10)
		var foundPurge bool
		for _, rec := range rep.Recommendations {
			if rec.Zone == "zone-office-a" && rec.Action == "purge" {
				foundPurge = true
				if rec.Metric != "co2" {
					t.Errorf("expected Metric == 'co2', got %q", rec.Metric)
				}
				if rec.Severity != "warning" && rec.Severity != "critical" {
					t.Errorf("expected warning/critical Severity, got %q", rec.Severity)
				}
			}
		}
		if !foundPurge {
			t.Fatalf("FALSE NEGATIVE: purge NOT recommended for CO2 spike")
		}
	})
}

// 4. Fuzz /api/command:
// Send 50KB payload, unicode/non-ASCII zones, SQL injection strings, JSON nulls, malformed numbers.
// Verify API behavior, panic freedom, and test memory leak impact on demoAssign map.
func TestAdversarial_FuzzCommandHandler(t *testing.T) {
	engine, _ := setupAdversarialTestEngine("zone-office-a")
	handler := commandHandler(engine)

	// Subtest 4.1: Malformed payloads, nulls, malformed numbers
	fuzzCases := []struct {
		name         string
		body         string
		expectedCode int
	}{
		{
			name:         "50KB malformed non-JSON payload",
			body:         strings.Repeat("X", 50*1024),
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "JSON null values for zone and command",
			body:         `{"zone": null, "command": null}`,
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "JSON null body",
			body:         `null`,
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Malformed numeric zone",
			body:         `{"zone": 12345, "command": "turn_off_ac"}`,
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Malformed numeric command",
			body:         `{"zone": "zone-office-a", "command": 99999}`,
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Malformed boolean command",
			body:         `{"zone": "zone-office-a", "command": true}`,
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Empty JSON object",
			body:         `{}`,
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Empty strings",
			body:         `{"zone": "", "command": ""}`,
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Whitespace strings",
			body:         `{"zone": "   ", "command": "   "}`,
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Missing command and action",
			body:         `{"zone": "zone-office-a"}`,
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Missing zone",
			body:         `{"command": "turn_off_ac"}`,
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, fc := range fuzzCases {
		t.Run(fc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewBufferString(fc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != fc.expectedCode {
				t.Fatalf("expected HTTP %d for %q, got %d: %s", fc.expectedCode, fc.name, rec.Code, rec.Body.String())
			}
		})
	}

	// Subtest 4.2: Fuzz Audit of Untrusted Zone Inputs (SQLi, Unicode, 50KB Zone Strings)
	// Testing empirical handling and memory leak potential in assignDemoZone.
	t.Run("Untrusted Zone Input Fuzz and Memory Audit", func(t *testing.T) {
		var m1, m2 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m1)

		untrustedInputs := []struct {
			name string
			zone string
		}{
			{"SQL Injection String 1", "'; DROP TABLE zones; --"},
			{"SQL Injection String 2", "1' OR '1'='1"},
			{"SQL Injection String 3", "admin'--"},
			{"Unicode / Non-ASCII 1", "🏢-办公区-LVL1"},
			{"Unicode / Non-ASCII 2", "❄️-hvac-control-日本語"},
			{"Unicode / Non-ASCII 3", "\u0000\u0001\u0002control"},
			{"50KB String Zone", strings.Repeat("A", 50*1024)},
		}

		for _, ui := range untrustedInputs {
			payload := map[string]string{
				"zone":    ui.zone,
				"command": "turn_off_ac",
			}
			data, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewBuffer(data))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			// Ensure no panic occurs
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("PANIC on untrusted input %q: %v", ui.name, r)
					}
				}()
				handler.ServeHTTP(rec, req)
			}()

			t.Logf("Untrusted Input %q: HTTP Code = %d, Response = %q", ui.name, rec.Code, rec.Body.String())
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 Bad Request for untrusted input %q, got %d", ui.name, rec.Code)
			}
		}

		runtime.GC()
		runtime.ReadMemStats(&m2)
		t.Logf("Memory footprint after untrusted zone tests: HeapAlloc Delta = %d bytes", int64(m2.HeapAlloc)-int64(m1.HeapAlloc))
	})

	// Subtest 4.3: Command Verb Whitelist Validation
	t.Run("Unsupported Command Verbs Rejected with 400", func(t *testing.T) {
		unsupportedCommands := []string{
			"malicious_eval",
			"reboot",
			"shutdown",
			"DROP TABLE zones",
			"eval(process.exit(1))",
			"rm -rf /",
			"random_action",
		}

		for _, cmd := range unsupportedCommands {
			payload := map[string]string{
				"zone":    "zone-office-a",
				"command": cmd,
			}
			data, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewBuffer(data))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 Bad Request for unsupported command %q, got %d", cmd, rec.Code)
			}
		}
	})
}

// TestAdversarial_CommandSecurityFuzzing empirically verifies /api/command safeguards
// against 50KB payloads, SQL injection, unicode/non-ASCII zones, and unsupported verbs.
func TestAdversarial_CommandSecurityFuzzing(t *testing.T) {
	TestAdversarial_FuzzCommandHandler(t)
}

