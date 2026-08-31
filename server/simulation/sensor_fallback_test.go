package simulation

import (
	"math"
	"testing"
	"time"
)

// TestOutdoorWeatherFallbackDynamicVsStatic asserts that when external weather is offline/stale,
// the simulation engine calculates dynamic diurnal temperature (25°C–34°C) and humidity (55%–95%)
// rather than falling back to a static flat constant.
func TestOutdoorWeatherFallbackDynamicVsStatic(t *testing.T) {
	ict := time.FixedZone("ICT", 7*3600)
	date := time.Date(2026, 8, 31, 0, 0, 0, 0, ict)

	// Sample 24-hour cycle at 3-hour intervals
	samples := make(map[int]struct {
		temp float64
		hum  float64
	})

	for h := 0; h < 24; h += 3 {
		ts := date.Add(time.Duration(h) * time.Hour)
		temp, hum := OutdoorFallbackAt(ts)
		samples[h] = struct {
			temp float64
			hum  float64
		}{temp, hum}
	}

	// 1. Afternoon peak at 15:00 must be maximum (~34.0°C)
	tPeak := samples[15].temp
	if math.Abs(tPeak-34.0) > 0.1 {
		t.Fatalf("at 15:00 (peak), want ~34.0°C, got %.2f°C", tPeak)
	}

	// 2. Night minimum at 03:00 must be minimum (~25.0°C)
	tMin := samples[3].temp
	if math.Abs(tMin-25.0) > 0.1 {
		t.Fatalf("at 03:00 (trough), want ~25.0°C, got %.2f°C", tMin)
	}

	// 3. Dynamic diurnal thermal swing must be at least 8.0°C
	swing := tPeak - tMin
	if swing < 8.0 {
		t.Fatalf("diurnal thermal swing %.2f°C is too flat; want >= 8.0°C", swing)
	}

	// 4. Relative humidity must inversely follow temperature (55% at 15:00, 95% at 03:00)
	hPeak := samples[15].hum
	hMin := samples[3].hum
	if math.Abs(hPeak-55.0) > 0.1 {
		t.Fatalf("at 15:00 (heat peak), want 55%% RH, got %.1f%%", hPeak)
	}
	if math.Abs(hMin-95.0) > 0.1 {
		t.Fatalf("at 03:00 (cool dawn), want 95%% RH, got %.1f%%", hMin)
	}

	// 5. Engine integration: when outdoorAt is zero (offline), outdoorNowAt must return diurnal curve
	e := newTestEngine()
	valAtNoon, liveNoon := e.outdoorNowAt(date.Add(15 * time.Hour))
	if liveNoon || math.Abs(valAtNoon-34.0) > 0.1 {
		t.Fatalf("engine outdoorNowAt(15:00) want 34.0°C/false, got %.2f°C/%v", valAtNoon, liveNoon)
	}

	valAtNight, liveNight := e.outdoorNowAt(date.Add(3 * time.Hour))
	if liveNight || math.Abs(valAtNight-25.0) > 0.1 {
		t.Fatalf("engine outdoorNowAt(03:00) want 25.0°C/false, got %.2f°C/%v", valAtNight, liveNight)
	}
}

// TestSolarGainDynamicPhysicsWhenLuxSensorOmitted asserts that when HwLux ambient light sensor
// is omitted, solar gain is dynamically derived from astronomical solar position geometry (Spencer/NOAA)
// and clear-sky GHI modeling (strictly 0.0 W at solar midnight, positive at solar noon).
func TestSolarGainDynamicPhysicsWhenLuxSensorOmitted(t *testing.T) {
	ict := time.FixedZone("ICT", 7*3600)
	date := time.Date(2026, 8, 31, 0, 0, 0, 0, ict)

	z := &ZoneSim{
		SolarGainMult: 1.0,
		LightsOn:      false,
		HwLuxAt:       time.Time{}, // sensor omitted
	}

	// 1. Midnight solar gain must be strictly 0.0 W (sun below horizon)
	midnight := date.Add(0 * time.Hour)
	gainMidnight := z.solarGainWAt(midnight)
	if gainMidnight != 0.0 {
		t.Fatalf("solar midnight (00:00): want strictly 0.0 W, got %.2f W (static mock rejection)", gainMidnight)
	}

	// 2. 03:00 solar gain must also be 0.0 W
	gain0300 := z.solarGainWAt(date.Add(3 * time.Hour))
	if gain0300 != 0.0 {
		t.Fatalf("early morning (03:00): want 0.0 W, got %.2f W", gain0300)
	}

	// 3. Solar noon (~12:00) must receive substantial clear-sky solar gain
	noon := date.Add(12 * time.Hour)
	gainNoon := z.solarGainWAt(noon)
	if gainNoon < 5000.0 {
		t.Fatalf("solar noon (12:00): want strong solar gain (> 5000 W), got %.2f W", gainNoon)
	}

	// 4. Afternoon (15:00) should have intermediate positive gain (0 < q < q_noon)
	gain1500 := z.solarGainWAt(date.Add(15 * time.Hour))
	if gain1500 <= 0 || gain1500 >= gainNoon {
		t.Fatalf("afternoon (15:00): want 0 < gain < gainNoon, got gain1500=%.2f gainNoon=%.2f", gain1500, gainNoon)
	}

	// 5. Interior zone with no aperture (SolarGainMult = 0.0) must remain 0.0 W at noon
	interior := &ZoneSim{SolarGainMult: 0.0}
	if got := interior.solarGainWAt(noon); got != 0.0 {
		t.Fatalf("interior zone with SolarGainMult=0.0 received %.2f W solar gain at noon", got)
	}
}

// TestCopAndCoolingPowerPhysicsWhenAcClampOmitted asserts that when HwAcW current clamp
// is omitted, chiller COP is calculated dynamically from thermodynamic lift (T_condenser - T_evaporator),
// Carnot limit, and thermal load, causing COP to degrade at higher ambient temperatures.
func TestCopAndCoolingPowerPhysicsWhenAcClampOmitted(t *testing.T) {
	const (
		tSupplyC     = 12.0
		thermalLoadW = 150000.0
		floorAreaM2  = 1200.0
		zeroStrain   = 0.0
	)

	// Scenario A: Mild ambient (25.0°C) -> low lift -> high COP
	copMild := CalculateThermodynamicCop(25.0, tSupplyC, thermalLoadW, floorAreaM2, zeroStrain)

	// Scenario B: Extreme ambient (38.0°C) -> high lift -> degraded COP
	copExtreme := CalculateThermodynamicCop(38.0, tSupplyC, thermalLoadW, floorAreaM2, zeroStrain)

	if copMild <= copExtreme {
		t.Fatalf("thermodynamic COP must degrade with higher ambient: copMild(25C)=%.3f copExtreme(38C)=%.3f",
			copMild, copExtreme)
	}

	degradationPct := (copMild - copExtreme) / copMild * 100.0
	if degradationPct < 15.0 {
		t.Fatalf("COP degradation from 25C to 38C was only %.1f%%; want >= 15.0%%", degradationPct)
	}

	// Electrical power requirement P = Q_thermal / COP
	pElectricalMild := thermalLoadW / copMild
	pElectricalExtreme := thermalLoadW / copExtreme
	if pElectricalExtreme <= pElectricalMild {
		t.Fatalf("extreme ambient must require more electrical power: pMild=%.1f W, pExtreme=%.1f W",
			pElectricalMild, pElectricalExtreme)
	}

	// Strain penalty: non-zero thermal strain must further degrade COP
	copStrained := CalculateThermodynamicCop(38.0, tSupplyC, thermalLoadW, floorAreaM2, 2.5)
	if copStrained >= copExtreme {
		t.Fatalf("thermal strain must reduce COP: unstrained=%.3f, strained=%.3f", copExtreme, copStrained)
	}
}

// TestSupplyAirTemperaturePhysicsWhenProbeOmitted asserts that when the DS18B20 supply probe
// is omitted, the engine calculates supply air temperature from mixed-air temperature and
// cooling coil heat exchange rather than returning a flat static 12.0°C.
func TestSupplyAirTemperaturePhysicsWhenProbeOmitted(t *testing.T) {
	e := newTestEngine()
	z := e.Zones["zone-office-a"]
	z.Temp = 26.0

	// Case 1: High outdoor load (38.0°C ambient) -> warmer mixed air -> warmer supply air
	tSupplyHot := e.calculateDynamicSupplyAir(38.0)

	// Case 2: Cool outdoor load (22.0°C ambient) -> cooler mixed air -> cooler supply air
	tSupplyCool := e.calculateDynamicSupplyAir(22.0)

	if tSupplyHot <= tSupplyCool {
		t.Fatalf("supply air temperature must dynamically reflect outdoor/mixed air load: hot=%.2f°C cool=%.2f°C",
			tSupplyHot, tSupplyCool)
	}

	// Verify physical plausibility bounds
	if tSupplyCool < 8.0 || tSupplyHot > 18.0 {
		t.Fatalf("supply air temperatures out of physical bounds [8°C, 18°C]: cool=%.2f°C hot=%.2f°C",
			tSupplyCool, tSupplyHot)
	}

	// Verify probe override: when a physical probe reports 13.5°C, it supersedes the dynamic calculation
	z.HwSupplyC, z.HwSupplyAt = 13.5, time.Now()
	if got := z.supplyCWithDefault(24.0, tSupplyHot); got != 13.5 {
		t.Fatalf("measured probe must supersede derived calculation: want 13.5, got %.2f", got)
	}
}

// TestZoneThermalCouplingWhenTemperatureSensorOmitted asserts that when temperature sensors
// are omitted, zone temperatures evolve dynamically through multi-zone 2R1C differential
// equations, including inter-zone partition heat transfer with adjacent rooms.
func TestZoneThermalCouplingWhenTemperatureSensorOmitted(t *testing.T) {
	e := newTestEngine()

	// Configure Zone A and Zone B as adjacent rooms
	zA := e.Zones["zone-office-a"]
	zB := e.Zones["zone-office-b"]

	zA.Temp = 24.0
	zA.WallTemp = 24.0
	zA.Setpoint = 24.0
	zA.AdjacentZones = []string{"zone-office-b"}

	// Heat adjacent Zone B to 32.0°C
	zB.Temp = 32.0
	zB.WallTemp = 32.0
	zB.Setpoint = 32.0

	// Hold outdoor temperature at 24.0°C so exterior envelope causes no heat gain
	e.SetOutdoorTemp(24.0)

	// Zero base and solar gains on Zone A
	zA.BaseHeatGain = 0.0
	zA.Occupancy = 0
	zA.SolarGainMult = 0.0

	// Close Zone A VAV damper so cooling does not immediately offset inter-zone flux
	if vA, ok := e.Vavs["vav-office-a"]; ok {
		vA.Flow = 0.0
	}

	// Integrate thermal step for 10 seconds
	dt := 1.0
	for i := 0; i < 10; i++ {
		e.tick(dt)
	}

	// Zone A temperature must rise due to conductive heat transfer from hot Zone B (32°C)
	if zA.Temp <= 24.0 {
		t.Fatalf("Zone A did not receive inter-zone heat transfer from Zone B: Temp=%.4f°C, want > 24.0°C", zA.Temp)
	}
}

// TestCO2MassBalanceWhenNdirSensorOmitted asserts that when NDIR CO2 sensors are omitted,
// zone CO2 concentrations dynamically integrate occupant generation rate and ventilation air changes.
func TestCO2MassBalanceWhenNdirSensorOmitted(t *testing.T) {
	e := newTestEngine()
	zA := e.Zones["zone-office-a"]
	zA.AreaM2 = 50.0
	zA.Occupancy = 10
	zA.Co2Sim = 400.0

	// Integrate 60 steps of 1.0s (1 minute of 10 occupants generating CO2)
	for i := 0; i < 60; i++ {
		e.tick(1.0)
	}

	// CO2 must increase above atmospheric baseline (400 ppm)
	if zA.Co2Sim <= 400.0 {
		t.Fatalf("occupied zone CO2 did not accumulate: Co2Sim=%.2f ppm, want > 400 ppm", zA.Co2Sim)
	}
	co2AfterOccupied := zA.Co2Sim

	// Now empty the room (0 occupants) and ventilate for 120 steps
	zA.Occupancy = 0
	for i := 0; i < 120; i++ {
		e.tick(1.0)
	}

	// CO2 concentration must decay toward baseline
	if zA.Co2Sim >= co2AfterOccupied {
		t.Fatalf("vacant ventilated zone CO2 did not decay: afterVacancy=%.2f ppm, was %.2f ppm",
			zA.Co2Sim, co2AfterOccupied)
	}

	// Whole-building avgCo2 must reflect the dynamic mass balance
	avg := e.avgCo2(0)
	if avg < 390.0 || avg > 2000.0 {
		t.Fatalf("building avgCo2 out of plausible bounds: %.2f ppm", avg)
	}
}
