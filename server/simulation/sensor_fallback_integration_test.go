package simulation

import (
	"math"
	"testing"
	"time"
)

// TestFullSensorOmissionDynamicSimulation verifies end-to-end simulation stability and
// physics accuracy when ALL physical sensors (temperature, occupancy, lux, AC clamp, plug clamp,
// supply probe, outdoor weather API) are omitted or offline.
func TestFullSensorOmissionDynamicSimulation(t *testing.T) {
	e := newTestEngine()

	// Ensure all zones have sensors omitted
	for _, z := range e.Zones {
		z.HwTempAt = time.Time{}
		z.HwHumAt = time.Time{}
		z.HwCo2At = time.Time{}
		z.HwSupplyAt = time.Time{}
		z.HwAcAt = time.Time{}
		z.HwLuxAt = time.Time{}
		z.HwPlugAt = time.Time{}
		z.Live = false
	}
	e.outdoorAt = time.Time{} // weather offline

	// Run 100 simulation ticks
	dt := 1.0
	for step := 0; step < 100; step++ {
		e.tick(dt)
		e.simClock += dt
		e.actuate()
		e.applyHardware()
	}

	// Verify all zone states are numerically stable and physically plausible
	for id, z := range e.Zones {
		if math.IsNaN(z.Temp) || math.IsInf(z.Temp, 0) {
			t.Fatalf("zone %s Temp is NaN/Inf", id)
		}
		if z.Temp < 10.0 || z.Temp > 45.0 {
			t.Fatalf("zone %s Temp out of plausible bounds: %.2f°C", id, z.Temp)
		}
		if math.IsNaN(z.WallTemp) || math.IsInf(z.WallTemp, 0) {
			t.Fatalf("zone %s WallTemp is NaN/Inf", id)
		}
		if math.IsNaN(z.Co2Sim) || math.IsInf(z.Co2Sim, 0) {
			t.Fatalf("zone %s Co2Sim is NaN/Inf", id)
		}
		if z.Co2Sim < 350.0 || z.Co2Sim > 5000.0 {
			t.Fatalf("zone %s Co2Sim out of bounds: %.2f ppm", id, z.Co2Sim)
		}
	}

	// Verify building-wide derived metrics
	load := e.buildingLoadForTest()
	if math.IsNaN(load) || math.IsInf(load, 0) || load <= 0 {
		t.Fatalf("building load is invalid: %.6f MW", load)
	}

	avgCo2 := e.avgCo2(0)
	if math.IsNaN(avgCo2) || math.IsInf(avgCo2, 0) || avgCo2 < 350.0 || avgCo2 > 2000.0 {
		t.Fatalf("building avgCo2 is invalid: %.2f ppm", avgCo2)
	}
}

// TestSensorDropoutAndGracefulPhysicsRecovery verifies that when a live physical sensor
// stops publishing telemetry (e.g. edge node disconnects), the engine seamlessly transitions
// back to pure physics ODE integration without numerical discontinuity.
func TestSensorDropoutAndGracefulPhysicsRecovery(t *testing.T) {
	e := newTestEngine()
	z := e.Zones["zone-office-a"]
	z.Setpoint = 24.0

	// 1. Ingest live sensor telemetry (node online)
	tempVal := 27.5
	luxVal := 850.0
	acVal := 2200.0
	supplyVal := 13.0

	z.HwTemp, z.HwTempAt = tempVal, time.Now()
	z.HwLux, z.HwLuxAt = luxVal, time.Now()
	z.HwAcW, z.HwAcAt = acVal, time.Now()
	z.HwSupplyC, z.HwSupplyAt = supplyVal, time.Now()

	// Apply hardware pull
	e.applyHardware()
	if !z.hwFresh() {
		t.Fatal("zone should be hardware-pinned while telemetry is fresh")
	}

	// 2. Simulate sensor timeout (node unplugged / silent for > 20 seconds)
	staleTime := time.Now().Add(-2 * hwStaleAfter)
	z.HwTempAt = staleTime
	z.HwLuxAt = staleTime
	z.HwAcAt = staleTime
	z.HwSupplyAt = staleTime

	if z.hwFresh() {
		t.Fatal("zone should no longer be hardware-pinned after staleness timeout")
	}

	// 3. Run physics steps after dropout
	for i := 0; i < 30; i++ {
		e.tick(1.0)
		e.applyHardware()
	}

	// Verify smooth transition back to 2R1C thermal model
	if math.IsNaN(z.Temp) || math.IsInf(z.Temp, 0) {
		t.Fatalf("zone Temp corrupted after sensor dropout: %.2f", z.Temp)
	}
	if z.Temp < 15.0 || z.Temp > 35.0 {
		t.Fatalf("zone Temp diverged unreasonably after sensor dropout: %.2f°C", z.Temp)
	}

	// Verify solar gain reverted to dynamic solar geometry
	expectedSolar := z.solarGainWAt(time.Now())
	if math.Abs(z.solarGainW()-expectedSolar) > 1e-6 {
		t.Fatalf("solar gain did not revert to dynamic solar geometry after sensor timeout: got %.2f W, want %.2f W",
			z.solarGainW(), expectedSolar)
	}
}

// TestMultiZoneCoupledPhysicsAndThermalBalance verifies inter-zone heat transfer across
// multiple connected rooms in a multi-zone layout.
func TestMultiZoneCoupledPhysicsAndThermalBalance(t *testing.T) {
	e := newTestEngine()

	// Link zone-office-a <-> zone-office-b <-> zone-office-c
	zA := e.Zones["zone-office-a"]
	zB := e.Zones["zone-office-b"]
	zC := e.Zones["zone-office-c"]

	zA.AdjacentZones = []string{"zone-office-b"}
	zB.AdjacentZones = []string{"zone-office-a", "zone-office-c"}
	zC.AdjacentZones = []string{"zone-office-b"}

	// Heat central Zone B to 36°C while A and C are at 22°C
	zA.Temp, zA.WallTemp = 22.0, 22.0
	zB.Temp, zB.WallTemp = 36.0, 36.0
	zC.Temp, zC.WallTemp = 22.0, 22.0

	// Close all dampers and hold ambient at 22°C
	e.SetOutdoorTemp(22.0)
	for _, z := range []*ZoneSim{zA, zB, zC} {
		z.BaseHeatGain = 0
		z.Occupancy = 0
		z.SolarGainMult = 0
	}

	for _, v := range e.Vavs {
		v.Flow = 0.0
	}

	// Integrate thermal physics for 20 steps
	for i := 0; i < 20; i++ {
		e.tick(1.0)
	}

	// Heat from central Zone B must conduct into adjacent Zones A and C
	if zA.Temp <= 22.0 {
		t.Fatalf("Zone A did not warm from adjacent Zone B: Temp=%.3f°C", zA.Temp)
	}
	if zC.Temp <= 22.0 {
		t.Fatalf("Zone C did not warm from adjacent Zone B: Temp=%.3f°C", zC.Temp)
	}
	if zB.Temp >= 36.0 {
		t.Fatalf("Zone B did not cool by transferring heat to adjacent zones: Temp=%.3f°C", zB.Temp)
	}
}
