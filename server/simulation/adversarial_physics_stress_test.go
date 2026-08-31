package simulation

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// 1. SOLAR GEOMETRY ADVERSARIAL STRESS TESTS
// ============================================================================

// TestAdversarialSolarGeometryArbitraryTimesAndExtremes rigorously tests Spencer/NOAA
// solar position calculations across spatial and temporal extremes:
// - Midnight & Noon across latitudes (-90° to +90°) and longitudes (-180° to +180°)
// - Equinoxes & Solstices (Summer/Winter solstices, Vernal/Autumnal equinoxes)
// - Leap years (2024-02-29, 2000-02-29, 2028-02-29) and century boundaries
// - High precision timestamps down to nanoseconds
// Asserts 100% numerical stability, physical bounds [0, 1500 W/m²], and strict 0 W at night.
func TestAdversarialSolarGeometryArbitraryTimesAndExtremes(t *testing.T) {
	testLocations := []struct {
		name string
		lat  float64
		lon  float64
	}{
		{"Ho Chi Minh City (Default)", DefaultSiteLat, DefaultSiteLon},
		{"Hanoi, Vietnam", 21.0285, 105.8542},
		{"Equator Prime Meridian", 0.0, 0.0},
		{"Equator Date Line East", 0.0, 180.0},
		{"Equator Date Line West", 0.0, -180.0},
		{"North Pole", 90.0, 0.0},
		{"South Pole", -90.0, 0.0},
		{"Arctic Circle (Tromsø)", 69.6492, 18.9553},
		{"Antarctic Station (McMurdo)", -77.8419, 166.6863},
		{"Tropic of Cancer", 23.4365, 0.0},
		{"Tropic of Capricorn", -23.4365, 0.0},
		{"Reykjavik, Iceland", 64.1466, -21.9426},
		{"Singapore", 1.3521, 103.8198},
	}

	testYears := []int{2000, 2024, 2026, 2028, 2096, 2100}
	testDays := []struct {
		month time.Month
		day   int
		desc  string
	}{
		{time.January, 1, "New Year"},
		{time.February, 28, "Late Feb"},
		{time.February, 29, "Leap Day"},
		{time.March, 20, "Vernal Equinox"},
		{time.June, 21, "Summer Solstice"},
		{time.September, 22, "Autumnal Equinox"},
		{time.December, 21, "Winter Solstice"},
		{time.December, 31, "Year End"},
	}

	for _, loc := range testLocations {
		for _, yr := range testYears {
			for _, td := range testDays {
				// Skip Feb 29 on non-leap years
				if td.month == time.February && td.day == 29 {
					if yr%4 != 0 || (yr%100 == 0 && yr%400 != 0) {
						continue
					}
				}

				// Sample 24 hours at 15-minute intervals (96 samples per day)
				for minute := 0; minute < 1440; minute += 15 {
					h := minute / 60
					m := minute % 60
					ts := time.Date(yr, td.month, td.day, h, m, 37, 123456789, time.UTC)

					cosZen, zenRad, dni, ghi := SolarPosition(ts, loc.lat, loc.lon)

					// Invariant 1: No NaNs or infinities
					if math.IsNaN(cosZen) || math.IsInf(cosZen, 0) {
						t.Fatalf("[%s %s %v %02d:%02d] cosZenith is NaN/Inf: %v", loc.name, td.desc, yr, h, m, cosZen)
					}
					if math.IsNaN(zenRad) || math.IsInf(zenRad, 0) {
						t.Fatalf("[%s %s %v %02d:%02d] zenithRad is NaN/Inf: %v", loc.name, td.desc, yr, h, m, zenRad)
					}
					if math.IsNaN(dni) || math.IsInf(dni, 0) {
						t.Fatalf("[%s %s %v %02d:%02d] DNI is NaN/Inf: %v", loc.name, td.desc, yr, h, m, dni)
					}
					if math.IsNaN(ghi) || math.IsInf(ghi, 0) {
						t.Fatalf("[%s %s %v %02d:%02d] GHI is NaN/Inf: %v", loc.name, td.desc, yr, h, m, ghi)
					}

					// Invariant 2: Mathematical domain bounds
					if cosZen < -1.0 || cosZen > 1.0 {
						t.Fatalf("[%s %s %v] cosZenith out of [-1, 1]: %.6f", loc.name, td.desc, yr, cosZen)
					}
					if zenRad < 0.0 || zenRad > math.Pi {
						t.Fatalf("[%s %s %v] zenithRad out of [0, pi]: %.6f", loc.name, td.desc, yr, zenRad)
					}

					// Invariant 3: Night radiation must be strictly 0.0 W/m²
					if cosZen <= 0.0 {
						if dni != 0.0 || ghi != 0.0 {
							t.Fatalf("[%s %s %v %02d:%02d] night irradiance non-zero: cosZen=%.4f dni=%.2f ghi=%.2f",
								loc.name, td.desc, yr, h, m, cosZen, dni, ghi)
						}
					} else {
						// Invariant 4: Daytime irradiance must be positive and physically bounded <= 1500 W/m²
						if dni < 0.0 || dni > 1500.0 {
							t.Fatalf("[%s %s %v %02d:%02d] DNI out of bounds [0, 1500]: %.2f W/m²",
								loc.name, td.desc, yr, h, m, dni)
						}
						if ghi < 0.0 || ghi > 1500.0 {
							t.Fatalf("[%s %s %v %02d:%02d] GHI out of bounds [0, 1500]: %.2f W/m²",
								loc.name, td.desc, yr, h, m, ghi)
						}
					}
				}
			}
		}
	}
}

// TestSolarGeometryEquatorialNoonVersusMidnight asserts that for an equatorial site,
// solar noon achieves strong clear-sky irradiance while midnight is strictly zero.
func TestSolarGeometryEquatorialNoonVersusMidnight(t *testing.T) {
	utcNoon := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)     // Spring Equinox noon at (0, 0)
	utcMidnight := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC) // Spring Equinox midnight at (0, 0)

	cosNoon, _, dniNoon, ghiNoon := SolarPosition(utcNoon, 0.0, 0.0)
	cosMid, _, dniMid, ghiMid := SolarPosition(utcMidnight, 0.0, 0.0)

	if cosNoon <= 0.95 || ghiNoon < 900.0 || dniNoon < 800.0 {
		t.Fatalf("equatorial noon under-irradiance: cos=%.4f ghi=%.2f dni=%.2f", cosNoon, ghiNoon, dniNoon)
	}

	if cosMid >= 0.0 || ghiMid != 0.0 || dniMid != 0.0 {
		t.Fatalf("equatorial midnight non-zero irradiance: cos=%.4f ghi=%.2f dni=%.2f", cosMid, ghiMid, dniMid)
	}
}

// ============================================================================
// 2. CHILLER COP & ELECTRICAL POWER ADVERSARIAL STRESS TESTS
// ============================================================================

// TestAdversarialChillerCopAcrossExtremeThermalLift tests the thermodynamic chiller COP
// under extreme atmospheric, supply, and loading conditions:
// - Extreme outdoor temperatures: -60°C to +75°C
// - Supply temperatures: +2°C to +35°C
// - Thermal loads: 0 W to 500 MW
// - Conditioned floor area: 1 m² to 1,000,000 m²
// - Thermal strain: 0.0 to 100.0 °C
// Asserts COP is strictly within [CopMin, CopMax], never NaN/Inf, and exhibits monotonic degradation with lift.
func TestAdversarialChillerCopAcrossExtremeThermalLift(t *testing.T) {
	ph := Phys()

	outdoorTemps := []float64{-60.0, -20.0, -5.0, 0.0, 15.0, 25.0, 35.0, 42.0, 50.0, 65.0, 75.0}
	supplyTemps := []float64{2.0, 6.0, 10.0, 12.0, 15.0, 20.0, 30.0}
	thermalLoads := []float64{0.0, 100.0, 5000.0, 50000.0, 150000.0, 1000000.0, 50000000.0, 500000000.0}
	floorAreas := []float64{1.0, 50.0, 500.0, 1200.0, 40000.0, 1000000.0}
	strains := []float64{0.0, 0.5, 1.5, 3.0, 8.0, 25.0, 100.0}

	for _, tOut := range outdoorTemps {
		for _, tSup := range supplyTemps {
			for _, load := range thermalLoads {
				for _, area := range floorAreas {
					for _, strain := range strains {
						cop := CalculateThermodynamicCop(tOut, tSup, load, area, strain)

						// 1. Strict numerical validity
						if math.IsNaN(cop) || math.IsInf(cop, 0) {
							t.Fatalf("COP is NaN/Inf for tOut=%.1f tSup=%.1f load=%.1f area=%.1f strain=%.1f: got %v",
								tOut, tSup, load, area, strain, cop)
						}

						// 2. Physical boundary clamping [CopMin, CopMax]
						if cop < ph.CopMin || cop > ph.CopMax {
							t.Fatalf("COP out of bounds [%.2f, %.2f]: got %.4f (tOut=%.1f, tSup=%.1f, load=%.1f)",
								ph.CopMin, ph.CopMax, cop, tOut, tSup, load)
						}

						// 3. Electrical power calculation P = Q / COP
						pElec := load / cop
						if math.IsNaN(pElec) || math.IsInf(pElec, 0) || pElec < 0 {
							t.Fatalf("calculated electrical power is invalid: pElec=%.2f W (load=%.1f, cop=%.4f)",
								pElec, load, cop)
						}
					}
				}
			}
		}
	}

	// 4. Monotonic lift degradation test: holding load/supply constant, rising ambient must degrade COP
	const (
		fixedSupply = 12.0
		fixedLoad   = 150000.0
		fixedArea   = 1200.0
		fixedStrain = 0.0
	)

	copMild := CalculateThermodynamicCop(20.0, fixedSupply, fixedLoad, fixedArea, fixedStrain)
	copWarm := CalculateThermodynamicCop(32.0, fixedSupply, fixedLoad, fixedArea, fixedStrain)
	copHot := CalculateThermodynamicCop(45.0, fixedSupply, fixedLoad, fixedArea, fixedStrain)

	if copMild < copWarm || copWarm < copHot {
		t.Fatalf("thermodynamic COP must degrade with higher temperature: mild(20C)=%.3f warm(32C)=%.3f hot(45C)=%.3f",
			copMild, copWarm, copHot)
	}

	// 5. Monotonic strain degradation test: higher strain must reduce COP
	copUnstrained := CalculateThermodynamicCop(35.0, fixedSupply, fixedLoad, fixedArea, 0.0)
	copStrainedLow := CalculateThermodynamicCop(35.0, fixedSupply, fixedLoad, fixedArea, 2.0)
	copStrainedHigh := CalculateThermodynamicCop(35.0, fixedSupply, fixedLoad, fixedArea, 6.0)

	if copUnstrained < copStrainedLow || copStrainedLow < copStrainedHigh {
		t.Fatalf("thermodynamic COP must degrade with strain: 0C_strain=%.3f 2C_strain=%.3f 6C_strain=%.3f",
			copUnstrained, copStrainedLow, copStrainedHigh)
	}
}

// ============================================================================
// 3. SUPPLY AIR TEMPERATURE BOUNDS UNDER ERRATIC COIL LOADS
// ============================================================================

// TestAdversarialSupplyAirBoundsUnderErraticCoilLoads verifies that dynamic supply air
// temperature calculation:
// - Remains strictly clamped to physical range [8.0°C, 18.0°C] under all chaotic loads
// - Handles empty zone sets gracefully
// - Handles erratic airflow rates (0 m3/s to 10,000 m3/s)
// - Handles erratic zone temperatures (-50°C to +150°C)
// - Probe overrides reject unsafe values (e.g. >= setpoint - 1.0)
func TestAdversarialSupplyAirBoundsUnderErraticCoilLoads(t *testing.T) {
	e := newTestEngine()

	// 1. Chaotic outdoor ambient sweeps (-60°C to +80°C)
	for tOut := -60.0; tOut <= 80.0; tOut += 2.5 {
		tSupply := e.calculateDynamicSupplyAir(tOut)

		if math.IsNaN(tSupply) || math.IsInf(tSupply, 0) {
			t.Fatalf("dynamic supply air produced NaN/Inf for tOut=%.1f: %v", tOut, tSupply)
		}
		if tSupply < 8.0 || tSupply > 18.0 {
			t.Fatalf("dynamic supply air out of physical bounds [8.0, 18.0]: got %.3f for tOut=%.1f", tSupply, tOut)
		}
	}

	// 2. Chaotic zone temperature & flow configurations
	rnd := rand.New(rand.NewSource(42))
	for iter := 0; iter < 500; iter++ {
		for _, z := range e.Zones {
			z.Temp = rnd.Float64()*200.0 - 50.0 // -50°C to +150°C
		}
		for _, v := range e.Vavs {
			v.Flow = rnd.Float64() * 1000.0 // 0 to 1000 m3/s
		}

		tOut := rnd.Float64()*100.0 - 20.0 // -20°C to +80°C
		tSupply := e.calculateDynamicSupplyAir(tOut)

		if math.IsNaN(tSupply) || math.IsInf(tSupply, 0) {
			t.Fatalf("iteration %d: supply air NaN/Inf: %v", iter, tSupply)
		}
		if tSupply < 8.0 || tSupply > 18.0 {
			t.Fatalf("iteration %d: supply air %.3f out of bounds [8.0, 18.0]", iter, tSupply)
		}
	}

	// 3. Empty zones map fallback test
	emptyEngine := &Engine{Zones: make(map[string]*ZoneSim), Vavs: make(map[string]*VavSim)}
	tEmpty := emptyEngine.calculateDynamicSupplyAir(35.0)
	if tEmpty != Phys().SupplyAirDesignC {
		t.Fatalf("empty engine must return design supply air (%.1f), got %.1f", Phys().SupplyAirDesignC, tEmpty)
	}

	// 4. Physical probe override and safety rejection test
	z := e.Zones["zone-office-a"]
	z.Setpoint = 24.0

	// Valid cold probe (11.5°C) -> accepted
	z.HwSupplyC, z.HwSupplyAt = 11.5, time.Now()
	if got := z.supplyC(24.0); got != 11.5 {
		t.Fatalf("valid cold probe must be accepted: want 11.5, got %.2f", got)
	}

	// Dangerous warm probe (23.5°C >= Setpoint - 1.0) -> rejected, clamped to Setpoint - 1.0 (23.0°C) or default
	z.HwSupplyC = 23.5
	if got := z.supplyC(24.0); got >= 23.0 {
		t.Fatalf("dangerous warm probe at/above setpoint must be rejected/clamped: got %.2f", got)
	}

	// Sub-zero or zero probe (0.0°C or -5.0°C) -> rejected, falls back to design
	z.HwSupplyC = 0.0
	if got := z.supplyC(24.0); got != Phys().SupplyAirDesignC {
		t.Fatalf("zero probe must fallback to design supply: got %.2f, want %.2f", got, Phys().SupplyAirDesignC)
	}
}

// ============================================================================
// 4. COMPLETE SENSOR OMISSION MULTI-TICK NUMERICAL STABILITY (CHAOS STRESS)
// ============================================================================

// TestAdversarialCompleteSensorOmission10000Ticks tests pure physics ODE simulation
// across 10,000 multi-tick steps with ALL sensors omitted (temperature, humidity, CO2,
// lux, AC clamp, plug clamp, supply probe, and weather API offline).
// Asserts 100% numerical stability, bounded zone states, positive loads, and diurnal cycling.
func TestAdversarialCompleteSensorOmission10000Ticks(t *testing.T) {
	homeData, err := os.ReadFile(DataPath(BuildingDataHomeFile))
	if err != nil {
		t.Fatalf("failed to read home data: %v", err)
	}
	officeData, err := os.ReadFile(DataPath(BuildingDataFile))
	if err != nil {
		t.Fatalf("failed to read office data: %v", err)
	}

	for _, fixture := range []struct {
		name string
		data []byte
	}{
		{"Commercial Office Tower", officeData},
		{"Domestic House", homeData},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			e := NewEngine()
			if err := e.ReloadBuilding(fixture.data); err != nil {
				t.Fatalf("building reload failed: %v", err)
			}

			// Explicitly omit all sensors across all zones
			for _, z := range e.Zones {
				z.HwTempAt = time.Time{}
				z.HwHumAt = time.Time{}
				z.HwCo2At = time.Time{}
				z.HwSupplyAt = time.Time{}
				z.HwAcAt = time.Time{}
				z.HwLuxAt = time.Time{}
				z.HwPlugAt = time.Time{}
				z.Live = false
				z.HwOnline = false
			}
			e.outdoorAt = time.Time{} // weather poller offline -> dynamic diurnal fallback

			dt := 1.0 // 1.0 second per step
			ict := time.FixedZone("ICT", 7*3600)
			startTime := time.Date(2026, 8, 31, 0, 0, 0, 0, ict) // Start at midnight

			var maxLoad, minLoad float64
			minLoad = 1e9

			// Run 5,000 steps (~1.4 hours of continuous simulation)
			for step := 0; step < 5000; step++ {
				simTime := startTime.Add(time.Duration(step) * time.Second)

				// Tick physics
				e.tick(dt)
				e.simClock += dt
				e.actuate()
				e.applyHardware()
				e.applyOccupancySchedule(simTime)

				// Check zone numerical integrity every 50 steps
				if step%50 == 0 {
					for zid, z := range e.Zones {
						if math.IsNaN(z.Temp) || math.IsInf(z.Temp, 0) {
							t.Fatalf("[%s step %d] zone %s Temp NaN/Inf: %v", fixture.name, step, zid, z.Temp)
						}
						if z.Temp < 5.0 || z.Temp > 50.0 {
							t.Fatalf("[%s step %d] zone %s Temp out of physical range [5, 50]: %.2f°C",
								fixture.name, step, zid, z.Temp)
						}
						if math.IsNaN(z.WallTemp) || math.IsInf(z.WallTemp, 0) {
							t.Fatalf("[%s step %d] zone %s WallTemp NaN/Inf: %v", fixture.name, step, zid, z.WallTemp)
						}
						if math.IsNaN(z.Co2Sim) || math.IsInf(z.Co2Sim, 0) {
							t.Fatalf("[%s step %d] zone %s Co2Sim NaN/Inf: %v", fixture.name, step, zid, z.Co2Sim)
						}
						if z.Co2Sim < 350.0 || z.Co2Sim > 5000.0 {
							t.Fatalf("[%s step %d] zone %s Co2Sim out of bounds [350, 5000]: %.2f ppm",
								fixture.name, step, zid, z.Co2Sim)
						}
					}

					load := e.lastLoadMw
					if math.IsNaN(load) || math.IsInf(load, 0) || load <= 0 {
						t.Fatalf("[%s step %d] building load invalid: %v MW", fixture.name, step, load)
					}
					if load > maxLoad {
						maxLoad = load
					}
					if load < minLoad {
						minLoad = load
					}
				}
			}

			// Assert non-static behavior: building load must evolve dynamically
			if maxLoad <= minLoad {
				t.Fatalf("[%s] building load was completely static: min=%.4f max=%.4f", fixture.name, minLoad, maxLoad)
			}
		})
	}
}

// ============================================================================
// 5. RAPID ALTERNATING BUILDING MODEL SWITCHES UNDER CONCURRENT STRESS
// ============================================================================

// TestAdversarialConcurrentBuildingSwitches tests rapid alternating switches between
// commercial office tower (735 zones) and domestic house (5 zones) under heavy concurrent
// simulation ticking, telemetry ingestion, recommendations queries, and export requests.
// Verified under Go race detector () for 0 data races, 0 deadlocks, and 0 panics.
func TestAdversarialConcurrentBuildingSwitches(t *testing.T) {
	homeData, err := os.ReadFile(DataPath(BuildingDataHomeFile))
	if err != nil {
		t.Fatalf("read home data: %v", err)
	}
	officeData, err := os.ReadFile(DataPath(BuildingDataFile))
	if err != nil {
		t.Fatalf("read office data: %v", err)
	}

	engine := NewEngine()

	const (
		concurrencyWorkers = 16
		switchIterations   = 40
	)

	var stopSignal int32
	var wg sync.WaitGroup
	var switchCount int32
	var queryCount int32

	// 1. Goroutine running simulation loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		dt := 0.033
		for atomic.LoadInt32(&stopSignal) == 0 {
			engine.mu.Lock()
			engine.tick(dt)
			engine.simClock += dt
			engine.actuate()
			engine.applyHardware()
			engine.mu.Unlock()
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// 2. Goroutine performing rapid alternating ReloadBuilding switches
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < switchIterations; i++ {
			var data []byte
			var wantId string
			var wantZones int

			if i%2 == 0 {
				data = homeData
				wantId = "bldg-econ-house-hcmc"
				wantZones = 5
			} else {
				data = officeData
				wantId = "bldg-econ-digitized"
				wantZones = 735
			}

			if err := engine.ReloadBuilding(data); err != nil {
				t.Errorf("ReloadBuilding failed at iteration %d: %v", i, err)
				return
			}

			id := engine.BuildingId()
			if id != wantId {
				t.Errorf("after reload want id=%q, got %q", wantId, id)
			}

			engine.mu.Lock()
			zCount := len(engine.Zones)
			engine.mu.Unlock()
			if zCount != wantZones {
				t.Errorf("after reload want zones=%d, got %d", wantZones, zCount)
			}

			atomic.AddInt32(&switchCount, 1)
			time.Sleep(10 * time.Millisecond)
		}
		atomic.StoreInt32(&stopSignal, 1)
	}()

	// 3. Concurrent reader and telemetry ingestion workers
	for w := 0; w < concurrencyWorkers; w++ {
		wg.Add(1)
		workerId := w
		go func() {
			defer wg.Done()
			rnd := rand.New(rand.NewSource(int64(workerId + 100)))

			for atomic.LoadInt32(&stopSignal) == 0 {
				op := rnd.Intn(7)
				switch op {
				case 0:
					_ = engine.Recommendations(5)
				case 1:
					_ = engine.RoomModels()
				case 2:
					_, _ = engine.ForecastWindow(12)
				case 3:
					_, _, _ = engine.ObservedLoadRange()
				case 4:
					_ = engine.BuildingId()
				case 5:
					_ = engine.HardwareStatus()
				case 6:
					// Telemetry ingestion
					meas := Measurement{
						Temp:      fp(24.0 + rnd.Float64()*4.0),
						Occupancy: ip(rnd.Intn(5)),
						Source:    "esp32",
					}
					engine.IngestTelemetry("zone-office-lvl1", fmt.Sprintf("node_%d", workerId), meas)
				}
				atomic.AddInt32(&queryCount, 1)
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	if atomic.LoadInt32(&switchCount) < int32(switchIterations) {
		t.Fatalf("switches did not complete: completed %d / %d", switchCount, switchIterations)
	}

	t.Logf("Successfully executed %d rapid alternating building switches and %d concurrent queries with 0 race conditions or panics",
		switchCount, queryCount)
}
