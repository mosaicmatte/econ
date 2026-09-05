package simulation

import (
	"testing"
	"time"
)

// The modelled occupancy of a room must follow the room. This is the regression test for
// rand.Intn(10) at zone construction: a uniform 0..9 people per zone, drawn once at boot,
// which put seven people in a 4.3 m2 bathroom, held a 72 m2 house at 28 occupants forever,
// and — because the count never changed — made vacancy, and therefore the entire
// occupancy-driven setback, impossible.

// withLibrary installs a known library for the duration of a test, so these assertions
// exercise the occupancy model itself rather than whatever file the working directory
// happens to resolve to. libOnce is marked done so no later call re-reads from disk.
func withLibrary(t *testing.T) {
	t.Helper()
	prev := lib
	libOnce.Do(func() {}) // consume the once; loadLibrary must not run underneath us

	density := func(v float64) *float64 { return &v }
	lib = libraryDoc{
		Version: 2,
		Physics: defaultLibrary().Physics,
		Occupancy: OccupancySchedule{
			JitterFraction:  0.15,
			ResampleMinutes: 20,
			Profiles: map[string][]float64{
				"office":            {0, 0, 0, 0, 0, 0, 0.10, 0.20, 0.95, 1, 1, 1, 0.50, 0.95, 0.95, 0.95, 0.95, 0.30, 0.10, 0.05, 0, 0, 0, 0},
				"meeting":           {0, 0, 0, 0, 0, 0, 0, 0.05, 0.30, 0.60, 0.60, 0.40, 0.20, 0.50, 0.60, 0.60, 0.40, 0.10, 0, 0, 0, 0, 0, 0},
				"residentialLiving": {0.10, 0.10, 0.10, 0.10, 0.10, 0.15, 0.50, 0.60, 0.20, 0.15, 0.15, 0.15, 0.20, 0.15, 0.15, 0.15, 0.20, 0.50, 1, 1, 1, 0.90, 0.60, 0.30},
				"transient":         make([]float64, 24),
			},
		},
		Programmes: map[string]Programme{
			"open-office":    {AreaPerOccupantM2: density(10), OccupancyProfile: "office"},
			"meeting-room":   {AreaPerOccupantM2: density(2.5), OccupancyProfile: "meeting"},
			"living":         {AreaPerOccupantM2: density(8), OccupancyProfile: "residentialLiving"},
			"bathroom":       {OccupancyProfile: "transient"},
			"circulation":    {OccupancyProfile: "transient"},
			"corridor":       {OccupancyProfile: "transient"},
			"plant-room":     {OccupancyProfile: "transient", Critical: true},
			"comms-room":     {OccupancyProfile: "transient", Critical: true},
			"store":          {OccupancyProfile: "transient"},
			"wet-core":       {OccupancyProfile: "transient"},
			"no-profile-yet": {AreaPerOccupantM2: density(10)},
		},
	}
	t.Cleanup(func() { lib = prev })
}

func TestDesignOccupancyFollowsAreaAndDensity(t *testing.T) {
	withLibrary(t)

	// Two rooms of the same programme, one four times the other, hold four times the
	// people. Under rand.Intn(10) they were indistinguishable.
	// Areas chosen off a .5 boundary so the assertion is about the scaling, not about
	// which way math.Round breaks a tie.
	small := DesignOccupancy("open-office", 30)
	large := DesignOccupancy("open-office", 120)
	if small <= 0 || large <= 0 {
		t.Fatalf("open-office should have a design occupancy, got %d and %d", small, large)
	}
	if large != 4*small {
		t.Errorf("design occupancy should scale with area: 30 m2 -> %d, 120 m2 -> %d", small, large)
	}

	// A meeting room is denser than an open-plan floor of the same size.
	if DesignOccupancy("meeting-room", 50) <= DesignOccupancy("open-office", 50) {
		t.Error("a meeting room should be denser than open-plan office of equal area")
	}
}

func TestProgrammesWithNoOccupantDensityHoldNobody(t *testing.T) {
	withLibrary(t)

	// These are the programmes whose library entry carries areaPerOccupantM2: null.
	// They have no standing population, and inventing one loads the plant with fresh
	// air for occupants who are not there.
	for _, typ := range []string{"bathroom", "circulation", "corridor", "plant-room", "comms-room", "store", "wet-core"} {
		if n := DesignOccupancy(typ, 40); n != 0 {
			t.Errorf("%s has no occupant density; design occupancy should be 0, got %d", typ, n)
		}
		for h := 0; h < 24; h++ {
			at := time.Date(2026, 8, 21, h, 0, 0, 0, vnLoc)
			if n := scheduledOccupancy(typ, 40, at); n != 0 {
				t.Errorf("%s at %02d:00 should be unoccupied, got %d", typ, h, n)
			}
		}
	}
}

func TestOccupancyVariesOverTheDayAndReachesZero(t *testing.T) {
	withLibrary(t)

	// The property the whole optimizer depends on: an office empties overnight. A
	// constant occupancy means no zone is ever vacant, so no setback ever fires.
	night := scheduledOccupancy("open-office", 200, time.Date(2026, 8, 21, 3, 0, 0, 0, vnLoc))
	if night != 0 {
		t.Errorf("an open-plan office at 03:00 should be empty, got %d", night)
	}
	day := scheduledOccupancy("open-office", 200, time.Date(2026, 8, 21, 10, 0, 0, 0, vnLoc))
	if day <= 0 {
		t.Errorf("an open-plan office at 10:00 should be occupied, got %d", day)
	}

	// And a residential living room runs the other way round: quiet mid-morning, full
	// in the evening. If both programmes had the same shape the profile would not be
	// doing any work.
	morning := OccupancyFractionAt("living", 10)
	evening := OccupancyFractionAt("living", 19)
	if !(evening > morning) {
		t.Errorf("a living room should be busier at 19:00 (%.2f) than at 10:00 (%.2f)", evening, morning)
	}
	if OccupancyFractionAt("open-office", 19) >= OccupancyFractionAt("open-office", 10) {
		t.Error("an office should be busier at 10:00 than at 19:00")
	}
}

func TestScheduledOccupancyNeverExceedsDesign(t *testing.T) {
	withLibrary(t)

	// The jitter is a variation around the schedule, not a licence to overfill a room.
	design := DesignOccupancy("open-office", 200)
	for i := 0; i < 2000; i++ {
		at := time.Date(2026, 8, 21, i%24, 0, 0, 0, vnLoc)
		n := scheduledOccupancy("open-office", 200, at)
		if n < 0 || n > design {
			t.Fatalf("occupancy %d out of range [0,%d] at hour %d", n, design, i%24)
		}
	}
}

func TestOccupancyScheduleNeverOverwritesAMeasurement(t *testing.T) {
	withLibrary(t)

	// Rule 1: a zone bound to a real occupancy sensor is not the model's to write.
	e := &Engine{Zones: map[string]*ZoneSim{
		"measured": {Type: "open-office", AreaM2: 200, Occupancy: 3, Live: true},
		"modelled": {Type: "open-office", AreaM2: 200, Occupancy: 3},
	}}
	e.applyOccupancySchedule(time.Date(2026, 8, 21, 10, 0, 0, 0, vnLoc))

	if got := e.Zones["measured"].Occupancy; got != 3 {
		t.Errorf("a sensor-backed zone must keep its measured occupancy, got %d", got)
	}
	if got := e.Zones["modelled"].Occupancy; got == 3 {
		t.Error("a modelled zone should have been rescheduled away from its seed value")
	}
}

func TestUnknownProgrammeIsUnoccupiedRatherThanInvented(t *testing.T) {
	withLibrary(t)

	// A digitizer may mint a programme the library has not been taught. The answer is
	// "nobody", not a plausible-looking headcount.
	if n := DesignOccupancy("programme-the-library-has-never-heard-of", 100); n != 0 {
		t.Errorf("unknown programme should hold nobody, got %d", n)
	}
	if f := OccupancyFractionAt("programme-the-library-has-never-heard-of", 12); f != 0 {
		t.Errorf("unknown programme should have no profile, got %.2f", f)
	}

	// A programme the library knows but has not given a profile is the same answer: it
	// has a density, but nothing says when anyone is there, so the engine does not guess.
	if f := OccupancyFractionAt("no-profile-yet", 12); f != 0 {
		t.Errorf("a programme with no occupancyProfile should contribute nobody, got %.2f", f)
	}
	if n := scheduledOccupancy("no-profile-yet", 100, time.Date(2026, 8, 21, 12, 0, 0, 0, vnLoc)); n != 0 {
		t.Errorf("a programme with no occupancyProfile should be unoccupied, got %d", n)
	}
}
