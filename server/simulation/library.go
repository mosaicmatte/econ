package simulation

// The programme library: every engineering coefficient the physics needs, loaded from
// data/programme-library.json instead of living as a literal in this package.
//
// The rule this file exists to enforce is that a number describing the BUILDING belongs
// in data, not in Go. A site with different lighting densities, a different ventilation
// code or a different plant COP should be re-calibrated by editing a JSON file that
// records where each figure came from — not by recompiling the engine. What stays in Go
// is the physics; what moves to data is every coefficient that physics is evaluated with.
//
// Fallbacks are deliberately conservative and are logged loudly. A missing library file
// must never silently substitute different physics — the twin says so and carries on.

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

// Programme is one space type: what the room is for, and what that implies.
type Programme struct {
	LightingWPerM2    float64  `json:"lightingWPerM2"`
	FixedEquipmentW   float64  `json:"fixedEquipmentW"`
	SetpointC         float64  `json:"setpointC"`
	DeadbandC         float64  `json:"deadbandC"`
	AreaPerOccupantM2 *float64 `json:"areaPerOccupantM2"`
	FacadeExposed     bool     `json:"facadeExposed"`
	// Critical spaces are never swept by the APLC and never set back by the optimizer.
	// One flag, read by both, so the two subsystems cannot disagree about which rooms
	// are untouchable — they did, and the setback was crediting itself with savings on
	// rooms the plug sweep already knew to leave alone.
	Critical bool `json:"critical"`
}

// Physics holds the coefficients the heat balance is evaluated with.
type Physics struct {
	AirRhoCpJPerM3K            float64 `json:"airRhoCpJPerM3K"`
	FurnishingCapMultiplier    float64 `json:"furnishingCapacitanceMultiplier"`
	OccupantSensibleW          float64 `json:"occupantSensibleW"`
	OutdoorAirLPerSPerPerson   float64 `json:"outdoorAirLPerSPerPerson"`
	VentilationEnthalpyKjPerKg float64 `json:"ventilationEnthalpyKjPerKg"`
	AirDensityKgPerM3          float64 `json:"airDensityKgPerM3"`
	SolarPeakWPerM2            float64 `json:"solarPeakWPerM2"`
	DesignCop                  float64 `json:"designCop"`
	SupplyAirDesignC           float64 `json:"supplyAirDesignC"`
	NonHvacBaseWPerM2          float64 `json:"nonHvacBaseWPerM2"`
	MinZoneCapacitanceJPerK    float64 `json:"minZoneCapacitanceJPerK"`
}

type libraryDoc struct {
	Version    int                  `json:"version"`
	Physics    Physics              `json:"physics"`
	Programmes map[string]Programme `json:"programmes"`
	Calibration struct {
		GridEmissionFactor float64 `json:"gridEmissionFactorTCo2PerMwh"`
	} `json:"calibration"`
}

var (
	libOnce sync.Once
	lib     libraryDoc
)

// defaultLibrary is what the engine falls back to when the file cannot be read. These are
// the same figures the shipped JSON carries; they exist so a stripped deployment or a
// unit test still has coherent physics, not so the file can be skipped.
func defaultLibrary() libraryDoc {
	var d libraryDoc
	d.Physics = Physics{
		AirRhoCpJPerM3K:            1206.0,
		FurnishingCapMultiplier:    5.0,
		OccupantSensibleW:          100.0,
		OutdoorAirLPerSPerPerson:   10.0,
		VentilationEnthalpyKjPerKg: 55.0,
		AirDensityKgPerM3:          1.2,
		SolarPeakWPerM2:            10.0,
		DesignCop:                  3.6,
		SupplyAirDesignC:           supplyAirC,
		NonHvacBaseWPerM2:          9.0,
		MinZoneCapacitanceJPerK:    5e4,
	}
	d.Programmes = map[string]Programme{}
	d.Calibration.GridEmissionFactor = 0.6766
	return d
}

// libraryPath is overridable so a site can point at its own calibration without moving
// the file the repository ships.
func libraryPath() string {
	if p := os.Getenv("ECON_PROGRAMME_LIBRARY"); p != "" {
		return p
	}
	return "./data/programme-library.json"
}

func loadLibrary() {
	lib = defaultLibrary()
	path := libraryPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[library] %s unreadable (%v) — falling back to built-in physics; "+
			"building coefficients are NOT site-calibrated", path, err)
		return
	}
	var doc libraryDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		log.Printf("[library] %s malformed (%v) — falling back to built-in physics", path, err)
		return
	}
	// Merge: any coefficient the file omits keeps the built-in value rather than
	// becoming zero, which would silently switch off a whole term of the heat balance.
	base := defaultLibrary().Physics
	p := doc.Physics
	mergeF(&p.AirRhoCpJPerM3K, base.AirRhoCpJPerM3K)
	mergeF(&p.FurnishingCapMultiplier, base.FurnishingCapMultiplier)
	mergeF(&p.OccupantSensibleW, base.OccupantSensibleW)
	mergeF(&p.OutdoorAirLPerSPerPerson, base.OutdoorAirLPerSPerPerson)
	mergeF(&p.VentilationEnthalpyKjPerKg, base.VentilationEnthalpyKjPerKg)
	mergeF(&p.AirDensityKgPerM3, base.AirDensityKgPerM3)
	mergeF(&p.SolarPeakWPerM2, base.SolarPeakWPerM2)
	mergeF(&p.DesignCop, base.DesignCop)
	mergeF(&p.SupplyAirDesignC, base.SupplyAirDesignC)
	mergeF(&p.NonHvacBaseWPerM2, base.NonHvacBaseWPerM2)
	mergeF(&p.MinZoneCapacitanceJPerK, base.MinZoneCapacitanceJPerK)
	doc.Physics = p
	if doc.Calibration.GridEmissionFactor == 0 {
		doc.Calibration.GridEmissionFactor = 0.6766
	}
	lib = doc
	crit := 0
	for _, pr := range lib.Programmes {
		if pr.Critical {
			crit++
		}
	}
	log.Printf("[library] loaded %s v%d: %d programmes (%d critical), fresh-air %.0f L/s/person",
		path, lib.Version, len(lib.Programmes), crit, lib.Physics.OutdoorAirLPerSPerPerson)
}

func mergeF(dst *float64, fallback float64) {
	if *dst == 0 {
		*dst = fallback
	}
}

// Lib returns the loaded programme library, reading it once on first use.
func Lib() *libraryDoc {
	libOnce.Do(loadLibrary)
	return &lib
}

// Phys is shorthand for the physics coefficients.
func Phys() Physics { return Lib().Physics }

// ProgrammeFor looks up a zone type. An unknown type is not an error — a digitized
// building may carry programmes the library has not been taught yet — so it returns the
// zero Programme and false, and callers fall back to behaviour that assumes nothing.
func ProgrammeFor(zoneType string) (Programme, bool) {
	p, ok := Lib().Programmes[zoneType]
	return p, ok
}

// IsCritical reports whether a zone type must never be swept or set back. Unknown types
// are treated as NOT critical, matching the previous behaviour, but the optimizer logs
// the first time it sets back a type the library has never heard of.
func IsCritical(zoneType string) bool {
	p, ok := ProgrammeFor(zoneType)
	return ok && p.Critical
}

// CriticalTypes lists every programme flagged critical — the single source both the
// plug sweep and the HVAC setback read.
func CriticalTypes() []string {
	out := []string{}
	for name, p := range Lib().Programmes {
		if p.Critical {
			out = append(out, name)
		}
	}
	return out
}

// GridEmissionFactor is tCO2 per MWh for the local grid, used to turn avoided energy
// into avoided carbon without that factor being retyped at every call site.
func GridEmissionFactor() float64 { return Lib().Calibration.GridEmissionFactor }
