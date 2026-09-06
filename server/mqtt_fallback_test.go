package main

import (
	"encoding/json"
	"math"
	"math/rand"
	"testing"
	"time"

	"econ/simulation"
)

// generateTestACWaveform generates a synthetic AC current waveform for testing offload fallback.
func generateTestACWaveform(rmsAmps float64, nSamples int, freqHz float64) []int {
	samples := make([]int, nSamples)
	calAPerV := 15.0
	dividerRatio := 0.5
	vPeakAdc := (rmsAmps * math.Sqrt(2.0) / calAPerV) * dividerRatio
	dcVolts := 1.25 // 2.5V quiescent / 2
	dt := 0.100 / float64(nSamples)

	rng := rand.New(rand.NewSource(12345))

	for i := 0; i < nSamples; i++ {
		t := float64(i) * dt
		vAc := vPeakAdc * math.Sin(2.0*math.Pi*freqHz*t)
		noise := rng.NormFloat64() * 3.0
		counts := (dcVolts+vAc)*(4095.0/3.3) + noise
		c := int(math.Round(counts))
		if c < 0 {
			c = 0
		}
		if c > 4095 {
			c = 4095
		}
		samples[i] = c
	}
	return samples
}

// TestMQTTFallbackDetectionAndDSP verifies that:
//  1. The backend detects "rawFallback": true in incoming MQTT telemetry.
//  2. It applies the simulation.CurrentDenoiser algorithm server-side.
//  3. It populates StripW in the zone state and the device registry (DB queue).
func TestMQTTFallbackDetectionAndDSP(t *testing.T) {
	ResetFallbackDenoisers()
	engine := simulation.NewEngine()

	targetAmps := 2.0
	targetWatts := 2.0 * 230.0 // 460 W nominal
	samples := generateTestACWaveform(targetAmps, 30, 50.0)

	payloadMap := map[string]interface{}{
		"zone":            "Level 4",
		"source":          "esp32",
		"cfgRev":          1,
		"temperature":     24.2,
		"tempReal":        true,
		"occupancy":       2,
		"rawFallback":     true,
		"rawStripSamples": samples,
		"lights":          "ON",
		"setpoint":        24.0,
		"acReal":          true,
	}

	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		t.Fatalf("Failed to marshal JSON payload: %v", err)
	}

	topic := "econ/telemetry/zone_1"
	handleTelemetry(engine, topic, payloadBytes)

	// 1. Verify zone state was updated with server-calculated StripW
	var matchedZone *simulation.ZoneSim
	for _, z := range engine.Zones {
		if z.HwStripW > 0 {
			matchedZone = z
			break
		}
	}

	if matchedZone == nil {
		t.Fatalf("Expected a zone with HwStripW > 0 after rawFallback ingestion, but none found")
	}

	if math.Abs(matchedZone.HwStripW-targetWatts) > 50.0 {
		t.Fatalf("Expected HwStripW around %.1f W (+/-50W), got %.1f W", targetWatts, matchedZone.HwStripW)
	}

	if time.Since(matchedZone.HwStripAt) > 5*time.Second {
		t.Fatalf("HwStripAt timestamp is not recent: %v", matchedZone.HwStripAt)
	}

	// 2. Verify device registry (and DB persistence queue) received the server-calculated StripW
	registry.mu.RLock()
	dev, ok := registry.d["zone_1"]
	registry.mu.RUnlock()
	if !ok {
		t.Fatalf("Expected device 'zone_1' in device registry")
	}

	fs, ok := dev.Fields["stripW"]
	if !ok {
		t.Fatalf("Expected 'stripW' metric in device registry fields")
	}
	if fs.Count == 0 {
		t.Fatalf("Expected stripW count > 0 in device registry")
	}
	if math.Abs(fs.Last-matchedZone.HwStripW) > 1e-3 {
		t.Fatalf("Device registry stripW last value %.1f differs from zone HwStripW %.1f",
			fs.Last, matchedZone.HwStripW)
	}
}

// TestMQTTFallbackZeroLoadNoiseFloor verifies that a 0A zero-signal waveform
// resolves cleanly to 0.0 W server-side without ghost readings.
func TestMQTTFallbackZeroLoadNoiseFloor(t *testing.T) {
	ResetFallbackDenoisers()
	engine := simulation.NewEngine()

	// Pure DC bias without AC swing
	samples := make([]int, 30)
	for i := range samples {
		samples[i] = 1550 // ~1.25V DC
	}

	payloadMap := map[string]interface{}{
		"zone":            "Level 4",
		"source":          "esp32",
		"rawFallback":     true,
		"rawStripSamples": samples,
	}
	payloadBytes, _ := json.Marshal(payloadMap)

	handleTelemetry(engine, "econ/telemetry/zone_1", payloadBytes)

	var matchedZone *simulation.ZoneSim
	for _, z := range engine.Zones {
		if z.HwOnline {
			matchedZone = z
			break
		}
	}

	if matchedZone == nil {
		t.Fatalf("Expected active zone after telemetry")
	}

	if matchedZone.HwStripW != 0.0 {
		t.Fatalf("Expected 0.0 W for zero load noise floor, got %.2f W", matchedZone.HwStripW)
	}
}

// TestMQTTNormalModeBypassesFallback verifies that when rawFallback is false or omitted,
// the server uses the edge node's published stripW directly without modifying it.
func TestMQTTNormalModeBypassesFallback(t *testing.T) {
	ResetFallbackDenoisers()
	engine := simulation.NewEngine()

	reportedWatts := 320.5
	isFallback := false

	payloadMap := map[string]interface{}{
		"zone":        "Level 4",
		"source":      "esp32",
		"rawFallback": isFallback,
		"stripW":      reportedWatts,
	}
	payloadBytes, _ := json.Marshal(payloadMap)

	handleTelemetry(engine, "econ/telemetry/zone_1", payloadBytes)

	var matchedZone *simulation.ZoneSim
	for _, z := range engine.Zones {
		if z.HwStripW == reportedWatts {
			matchedZone = z
			break
		}
	}

	if matchedZone == nil {
		t.Fatalf("Expected zone HwStripW to match reported watts %.1f directly", reportedWatts)
	}
}

// TestMQTTFallbackStarvationHandling verifies that starved raw sample buffers (< 20)
// do not publish erroneous power numbers.
func TestMQTTFallbackStarvationHandling(t *testing.T) {
	ResetFallbackDenoisers()
	engine := simulation.NewEngine()

	// Only 5 samples (starvation)
	starvedSamples := []int{1550, 1552, 1548, 1551, 1549}

	payloadMap := map[string]interface{}{
		"zone":            "Level 4",
		"source":          "esp32",
		"rawFallback":     true,
		"rawStripSamples": starvedSamples,
	}
	payloadBytes, _ := json.Marshal(payloadMap)

	handleTelemetry(engine, "econ/telemetry/zone_1", payloadBytes)

	// In starvation, StripW is not calculated and should remain 0/unset
	for _, z := range engine.Zones {
		if z.HwStripW > 0 {
			t.Fatalf("Expected no positive HwStripW on starved window, got %.2f", z.HwStripW)
		}
	}
}
