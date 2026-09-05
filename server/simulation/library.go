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
	"math"
	"os"
	"sort"
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
	// OccupancyProfile names the diurnal profile in the library's occupancy block that
	// says what fraction of this programme's DESIGN occupancy is present each hour. Empty
	// means the programme has not been given one, and the engine leaves such a zone
	// unoccupied rather than inventing a population for it.
	OccupancyProfile string `json:"occupancyProfile"`
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
	// SolarGainReferenceW is the solar gain a zone with an aperture multiplier of 1.0
	// receives. It is the coefficient a measured illuminance scales; DaylightReferenceLux
	// is the indoor illuminance that corresponds to it, so lux/reference is a pure ratio.
	SolarGainReferenceW  float64 `json:"solarGainReferenceW"`
	DaylightReferenceLux float64 `json:"daylightReferenceLux"`
	// CopStrainSlope is how far the plant's COP falls per °C of average zone strain, and
	// CopMin/CopMax bound the resulting curve. Previously literals in broadcast().
	CopStrainSlope          float64 `json:"copStrainSlope"`
	CopMin                  float64 `json:"copMin"`
	CopMax                  float64 `json:"copMax"`
	NonHvacBaseWPerM2       float64 `json:"nonHvacBaseWPerM2"`
	MinZoneCapacitanceJPerK float64 `json:"minZoneCapacitanceJPerK"`
	RInFraction             float64 `json:"rInFraction"`
	// HvacLoadPerDegCFraction and PrecoolShiftFraction are read by the DASHBOARD, not by
	// the heat balance: they price what relaxing a setpoint by a degree costs, and what a
	// pre-cool window can coast off the peak. They live here because they describe the
	// building's plant, and the panels that used to carry them as JavaScript literals had
	// drifted into three copies with three different justifications.
	HvacLoadPerDegCFraction float64 `json:"hvacLoadPerDegCFraction"`
	PrecoolShiftFraction    float64 `json:"precoolShiftFraction"`
	// OutdoorCo2Ppm and Co2PpmPerOccupantSteady back the MODELLED concentration shown
	// where no NDIR sensor reports. They were literals in engine.go and, with different
	// values, in three dashboard files. A surface that renders them must say "modelled".
	OutdoorCo2Ppm           float64 `json:"outdoorCo2Ppm"`
	Co2PpmPerOccupantSteady float64 `json:"co2PpmPerOccupantSteady"`
	// Air-side sizing. SupplyAirDesignAch gives every VAV a design flow from its own
	// zone's volume; AhuDesignPressurePa is the static those resistances are referenced
	// to and the operating point the fan curve is scaled to reach.
	SupplyAirDesignAch  float64 `json:"supplyAirDesignAch"`
	AhuDesignPressurePa float64 `json:"ahuDesignPressurePa"`
}

// OccupancySchedule is how many people the engine puts in a room, and when.
//
// This replaced a literal `rand.Intn(10)` at zone construction — a uniform 0..9 people
// per zone, drawn once at boot and never changed again. It ignored the room: a 4 m2
// bathroom and a 200 m2 open-plan floor drew from the same distribution, and the library's
// own areaPerOccupantM2 — the coefficient that exists to answer exactly this — went unread.
// Three things followed from it. The ventilation and occupant-gain terms dominated the heat
// balance with a population the building could not hold, so every load-derived figure on the
// dashboard was wrong. The count never changed, so no zone ever became vacant and the
// occupancy-driven setback — the saving this project is built to demonstrate — could not
// fire at all. And a constant regressor carries no information, so the occupant term of each
// room's identified thermal model was never excited.
//
// Design occupancy is the zone's own digitized area over its programme's areaPerOccupantM2.
// The profile scales it by hour. Both are coefficients that describe the building, so both
// live in the library (rule 2), and a zone bound to a real occupancy sensor ignores all of
// it (rule 1: a measurement always beats the model).
type OccupancySchedule struct {
	// Profiles maps a profile name to 24 hourly fractions of design occupancy.
	Profiles map[string][]float64 `json:"profiles"`
	// JitterFraction is the standard deviation of the hour-to-hour draw as a fraction of
	// the scheduled count. Real counts vary; a perfectly repeating schedule collapses
	// every learned baseline's spread to zero, which makes the first genuine variation
	// score as an unbounded anomaly.
	JitterFraction float64 `json:"jitterFraction"`
	// ResampleMinutes is how often the count is redrawn.
	ResampleMinutes float64 `json:"resampleMinutes"`
}

// StorageSizing describes the modelled battery a site gets when it has not declared a
// real one. A BESS is an asset rather than a physical coefficient, so a deployment that
// owns a pack sets its nameplate in the environment and none of this is read. These
// figures exist because the default was a flat 2 MW / 4 MWh — realistic for the ~17 MW
// campus the engine was first written against, and left unchanged when it was pointed at
// a 72 m2 house, where it was eighty times the entire building load, discharged the whole
// of it through every evening peak, and reported a grid draw of zero.
type StorageSizing struct {
	PowerFractionOfObservedPeak float64 `json:"powerFractionOfObservedPeak"`
	HoursAtRatedPower           float64 `json:"hoursAtRatedPower"`
	MinPowerMw                  float64 `json:"minPowerMw"`
}

type libraryDoc struct {
	Version     int                  `json:"version"`
	Physics     Physics              `json:"physics"`
	Occupancy   OccupancySchedule    `json:"occupancy"`
	Storage     StorageSizing        `json:"storage"`
	Programmes  map[string]Programme `json:"programmes"`
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
		RInFraction:                0.4,
		SolarGainReferenceW:        10000.0,
		DaylightReferenceLux:       1000.0,
		CopStrainSlope:             0.35,
		CopMin:                     2.2,
		CopMax:                     3.8,
		HvacLoadPerDegCFraction:    0.05,
		PrecoolShiftFraction:       0.05,
		OutdoorCo2Ppm:              400.0,
		Co2PpmPerOccupantSteady:    15.0,
		SupplyAirDesignAch:         6.0,
		AhuDesignPressurePa:        480.0,
	}
	d.Programmes = map[string]Programme{}
	// No built-in profiles: a stripped deployment gets a building with nobody in it,
	// which is wrong but visibly and safely wrong. Inventing a population here would put
	// this file back in the business of describing the building.
	d.Occupancy = OccupancySchedule{
		Profiles:        map[string][]float64{},
		JitterFraction:  0.15,
		ResampleMinutes: 20,
	}
	d.Storage = StorageSizing{
		PowerFractionOfObservedPeak: 0.30,
		HoursAtRatedPower:           2.0,
		MinPowerMw:                  0.001,
	}
	d.Calibration.GridEmissionFactor = 0.6766
	return d
}

// libraryPath is overridable so a site can point at its own calibration without moving
// the file the repository ships.
func libraryPath() string {
	if p := os.Getenv("ECON_PROGRAMME_LIBRARY"); p != "" {
		return p
	}
	// Local-first (datapath.go). Site calibration is the documented purpose of this file,
	// so a deployment that has calibrated it keeps its own copy in programme-library.local.json
	// — gitignored, and therefore safe from a pull that updates the shipped defaults.
	return DataPath(ProgrammeLibraryFile)
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
	mergeF(&p.RInFraction, base.RInFraction)
	mergeF(&p.SolarGainReferenceW, base.SolarGainReferenceW)
	mergeF(&p.DaylightReferenceLux, base.DaylightReferenceLux)
	mergeF(&p.CopStrainSlope, base.CopStrainSlope)
	mergeF(&p.CopMin, base.CopMin)
	mergeF(&p.CopMax, base.CopMax)
	mergeF(&p.HvacLoadPerDegCFraction, base.HvacLoadPerDegCFraction)
	mergeF(&p.PrecoolShiftFraction, base.PrecoolShiftFraction)
	mergeF(&p.OutdoorCo2Ppm, base.OutdoorCo2Ppm)
	mergeF(&p.Co2PpmPerOccupantSteady, base.Co2PpmPerOccupantSteady)
	mergeF(&p.SupplyAirDesignAch, base.SupplyAirDesignAch)
	mergeF(&p.AhuDesignPressurePa, base.AhuDesignPressurePa)
	doc.Physics = p
	baseOcc := defaultLibrary().Occupancy
	if doc.Occupancy.Profiles == nil {
		doc.Occupancy.Profiles = map[string][]float64{}
	}
	mergeF(&doc.Occupancy.JitterFraction, baseOcc.JitterFraction)
	mergeF(&doc.Occupancy.ResampleMinutes, baseOcc.ResampleMinutes)
	// A profile that is not 24 hours long cannot be indexed by hour. Drop it loudly
	// rather than let it silently index out of range or wrap onto the wrong hour.
	for name, prof := range doc.Occupancy.Profiles {
		if len(prof) != 24 {
			log.Printf("[library] occupancy profile %q has %d hours, not 24 — ignoring it; "+
				"zones using it will be modelled as unoccupied", name, len(prof))
			delete(doc.Occupancy.Profiles, name)
		}
	}
	baseStore := defaultLibrary().Storage
	mergeF(&doc.Storage.PowerFractionOfObservedPeak, baseStore.PowerFractionOfObservedPeak)
	mergeF(&doc.Storage.HoursAtRatedPower, baseStore.HoursAtRatedPower)
	mergeF(&doc.Storage.MinPowerMw, baseStore.MinPowerMw)
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
	log.Printf("[library] loaded %s v%d: %d programmes (%d critical), fresh-air %.0f L/s/person, "+
		"%d occupancy profiles",
		path, lib.Version, len(lib.Programmes), crit, lib.Physics.OutdoorAirLPerSPerPerson,
		len(lib.Occupancy.Profiles))
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

// Storage returns the modelled-battery sizing rules.
func Storage() StorageSizing { return Lib().Storage }

// Occupancy returns the loaded diurnal occupancy schedule.
func Occupancy() OccupancySchedule { return Lib().Occupancy }

// DesignOccupancy is how many people a zone of this programme and this floor area is
// designed to hold, at full occupancy.
//
// Zero has a specific meaning here and is not a failure: a programme with no
// areaPerOccupantM2 — a bathroom, a passage, a plant room — has no standing population.
// People pass through, but there is no design count for them, and inventing one loads the
// plant with fresh air for occupants who are not there. Such a room gets its occupancy
// from a real sensor or not at all.
func DesignOccupancy(zoneType string, areaM2 float64) int {
	p, ok := ProgrammeFor(zoneType)
	if !ok || p.AreaPerOccupantM2 == nil || *p.AreaPerOccupantM2 <= 0 || areaM2 <= 0 {
		return 0
	}
	n := int(math.Round(areaM2 / *p.AreaPerOccupantM2))
	// A room big enough to be a room at all holds at least one person. Rounding a 9 m2
	// living space at 8 m2/person down to nobody would make the twin claim the household
	// never uses its own living room.
	if n < 1 {
		n = 1
	}
	return n
}

// OccupancyFractionAt is the share of design occupancy present at the given hour for a
// programme. It returns 0 for a programme with no profile — the same "no standing
// population" answer as DesignOccupancy, reached the same deliberate way.
func OccupancyFractionAt(zoneType string, hour int) float64 {
	p, ok := ProgrammeFor(zoneType)
	if !ok || p.OccupancyProfile == "" {
		return 0
	}
	prof, ok := Lib().Occupancy.Profiles[p.OccupancyProfile]
	if !ok || len(prof) != 24 {
		return 0
	}
	if hour < 0 || hour > 23 {
		return 0
	}
	f := prof[hour]
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// GridEmissionFactor is tCO2 per MWh for the local grid, used to turn avoided energy
// into avoided carbon without that factor being retyped at every call site.
func GridEmissionFactor() float64 { return Lib().Calibration.GridEmissionFactor }

// --- read surface for the dashboard -----------------------------------------

// LibraryView is what /api/library serves: the coefficients and the programme facts a
// UI legitimately needs, and nothing else.
//
// It exists because the panels had grown their own copies of building constants — a
// supply-air temperature typed as 12.0, a 5%-per-degC rule of thumb repeated three times
// with three different explanations, and a hand-written list of which zone types are
// "critical" that still named `server-room` and `mechanical` long after the digitizer
// started minting `comms-room` and `plant-room`. Every one of those was a number
// describing the BUILDING living in a .go or .jsx file, which is the thing this package
// exists to prevent. Serving the library closes the loop: the dashboard evaluates the
// same coefficients the engine does, from the same file, or it says it could not.
type LibraryView struct {
	Version int  `json:"version"`
	Loaded  bool `json:"loaded"`
	// BuildingId and OccupancyModelVersion together identify the series the engine is
	// currently producing. The dashboard accumulates its own windows in localStorage — an
	// observed peak, a mean load — and those windows are subject to exactly the rule the
	// engine applies to its own persisted state: a window recorded from a different
	// building, or from this one under a different occupancy model, is not this series'
	// history. Without them a browser kept showing "the highest seen" from a run whose
	// load was five times what the building now draws, on a bar labelled as this
	// building's own observed peak.
	BuildingId            string                     `json:"buildingId"`
	OccupancyModelVersion int                        `json:"occupancyModelVersion"`
	Physics               Physics                    `json:"physics"`
	Programmes            map[string]ProgrammeFacing `json:"programmes"`
	// Critical is the flattened list of critical zone types — the single answer to "may
	// this room be swept or set back?" that both the engine and the UI now read.
	Critical []string `json:"critical"`
}

// ProgrammeFacing is the subset of a Programme a dashboard can use without pretending to
// re-run the physics.
type ProgrammeFacing struct {
	SetpointC      float64 `json:"setpointC"`
	DeadbandC      float64 `json:"deadbandC"`
	Critical       bool    `json:"critical"`
	LightingWPerM2 float64 `json:"lightingWPerM2"`
	FacadeExposed  bool    `json:"facadeExposed"`
	// AreaPerOccupantM2 is nil for programmes that are not occupied on a density basis
	// (a plant room, a store). Null rather than zero so a client can withhold a design
	// capacity instead of computing one from a divide-by-zero.
	AreaPerOccupantM2 *float64 `json:"areaPerOccupantM2"`
}

// Library returns the view above. Loaded reports whether the JSON was actually read: a
// dashboard shown built-in fallback physics must be able to say so rather than present
// uncalibrated defaults as the site's own.
func Library(buildingId string) LibraryView {
	l := Lib()
	out := LibraryView{
		Version:               l.Version,
		Loaded:                len(l.Programmes) > 0,
		BuildingId:            buildingId,
		OccupancyModelVersion: OccupancyModelVersion,
		Physics:               l.Physics,
		Programmes:            make(map[string]ProgrammeFacing, len(l.Programmes)),
		Critical:              CriticalTypes(),
	}
	for name, p := range l.Programmes {
		out.Programmes[name] = ProgrammeFacing{
			SetpointC:         p.SetpointC,
			DeadbandC:         p.DeadbandC,
			Critical:          p.Critical,
			LightingWPerM2:    p.LightingWPerM2,
			FacadeExposed:     p.FacadeExposed,
			AreaPerOccupantM2: p.AreaPerOccupantM2,
		}
	}
	sort.Strings(out.Critical) // stable output so a client can diff two fetches
	return out
}
