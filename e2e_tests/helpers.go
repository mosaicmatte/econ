package e2e_tests

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"econ/simulation"
)

// GenerateSineWave synthesizes ADC counts for an AC sinusoidal current.
// Matches the physical sensor characteristics of ESP32 ADC1 with ACS712 / SCT-013.
func GenerateSineWave(
	rmsAmps float64,
	calAPerV float64,
	dividerRatio float64,
	sampleRateHz float64,
	durationSec float64,
	freqHz float64,
	dcVolts float64,
	noiseSigma float64,
	seed int64,
) []int {
	nSamples := int(durationSec * sampleRateHz)
	if nSamples <= 0 {
		nSamples = 100
	}
	samples := make([]int, nSamples)

	// Peak voltage at the ADC pin after voltage divider
	vPeakAdc := (rmsAmps * math.Sqrt(2.0) / calAPerV) * dividerRatio
	dt := 1.0 / sampleRateHz

	rng := rand.New(rand.NewSource(seed))

	for i := 0; i < nSamples; i++ {
		t := float64(i) * dt
		vAc := vPeakAdc * math.Sin(2.0*math.Pi*freqHz*t)
		vTotal := dcVolts + vAc
		noise := rng.NormFloat64() * noiseSigma
		rawCount := vTotal*(4095.0/3.3) + noise
		count := int(math.Round(rawCount))
		if count < 0 {
			count = 0
		}
		if count > 4095 {
			count = 4095
		}
		samples[i] = count
	}
	return samples
}

// SetupTestEngine initializes an in-memory simulation engine with standard building zones.
func SetupTestEngine() *simulation.Engine {
	engine := simulation.NewEngine()
	engine.Zones = make(map[string]*simulation.ZoneSim)

	irActive := "COOL_24"
	irOff := "OFF"

	// Zone 1: Open Office (standard occupancy, active HVAC)
	engine.Zones["zone-office-a"] = &simulation.ZoneSim{
		Type:      "office",
		Temp:      24.0,
		Setpoint:  24.0,
		AreaM2:    60.0,
		Occupancy: 5,
		LightsOn:  true,
		IrState:   &irActive,
		MqttTopic: "zone_1",
	}

	// Zone 2: Executive Boardroom (intermittent occupancy, default AC OFF)
	engine.Zones["zone-boardroom"] = &simulation.ZoneSim{
		Type:      "meeting",
		Temp:      23.5,
		Setpoint:  23.5,
		AreaM2:    40.0,
		Occupancy: 0,
		LightsOn:  false,
		IrState:   &irOff,
		MqttTopic: "zone_2",
	}

	// Zone 3: Server Room (continuous equipment load, 0 occupancy, AC OFF for occupancy recs)
	engine.Zones["zone-server-room"] = &simulation.ZoneSim{
		Type:      "datacenter",
		Temp:      20.0,
		Setpoint:  20.0,
		AreaM2:    30.0,
		Occupancy: 0,
		LightsOn:  true,
		IrState:   &irOff,
		MqttTopic: "zone_3",
	}

	return engine
}

// TrainOccupancyBaseline folds N observations into the zone's occupancy baseline.
func TrainOccupancyBaseline(engine *simulation.Engine, zone string, mean float64, std float64, numObservations int, now time.Time) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < numObservations; i++ {
		val := mean + rng.NormFloat64()*std
		if val < 0 {
			val = 0
		}
		engine.ObserveBaseline(zone, "occupancy", val, now)
	}
}

// FindZoneRecommendation searches the list of recommendations for a specific zone, metric, and action.
func FindZoneRecommendation(recs []simulation.Recommendation, zone string, metric string, action string) *simulation.Recommendation {
	for i := range recs {
		if (zone == "" || recs[i].Zone == zone) &&
			(metric == "" || recs[i].Metric == metric) &&
			(action == "" || recs[i].Action == action) {
			return &recs[i]
		}
	}
	return nil
}

// AssertRecommendationBasis checks whether a recommendation matches the expected basis ("learned" or "standard").
func AssertRecommendationBasis(t *testing.T, rec *simulation.Recommendation, expectedBasis string) {
	t.Helper()
	if rec == nil {
		t.Fatalf("expected recommendation but got nil")
	}
	if rec.Basis != expectedBasis {
		t.Fatalf("expected recommendation Basis=%q, got Basis=%q (id=%s, msg=%s)",
			expectedBasis, rec.Basis, rec.Id, rec.Message)
	}
}
