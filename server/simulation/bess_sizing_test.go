package simulation

import (
	"testing"
)

// The modelled battery has to be a battery this building could plausibly have. The default
// was a flat 2 MW / 4 MWh pack, which is a fair description of storage on the ~17 MW campus
// this engine was written against and a fabrication on a 72 m2 house — eighty times the
// entire building load, discharging all of it through every evening peak, so the dashboard
// showed a house running off a battery it does not own with a grid draw of zero.

func TestUndeclaredPackIsSizedToTheBuilding(t *testing.T) {
	b := Battery{}
	// A house observed to peak at 25 kW.
	b.SizeToBuilding(0.025)

	if !b.Enabled {
		t.Fatal("a pack sized to a real observed peak should be enabled")
	}
	if b.PowerMw >= 0.025 {
		t.Errorf("inverter rating %.4f MW should be a fraction of the %.3f MW peak, not all of it",
			b.PowerMw, 0.025)
	}
	// It must never be able to carry the entire building, which is what made grid draw
	// read zero right through the peak window.
	b.Dispatch(60, 0.025, "peak")
	if b.DischargeMw >= 0.025 {
		t.Errorf("discharge %.4f MW covers the whole %.3f MW load; the grid draw would read zero",
			b.DischargeMw, 0.025)
	}

	// And the same rule at the other end of the range the engine runs at.
	tower := Battery{}
	tower.SizeToBuilding(1.5)
	if !(tower.PowerMw > b.PowerMw*10) {
		t.Errorf("a 1.5 MW building should get a far larger pack than a 0.025 MW one: %.4f vs %.4f",
			tower.PowerMw, b.PowerMw)
	}
	if hours := tower.CapacityMwh / tower.PowerMw; hours < 1.5 || hours > 4 {
		t.Errorf("pack duration %.2f h is outside the commercial range the library specifies", hours)
	}
}

func TestDeclaredNameplateIsNeverResized(t *testing.T) {
	// A site that owns a pack has stated a real asset. The twin does not second-guess it.
	b := Battery{Declared: true, Enabled: true, PowerMw: 2.0, CapacityMwh: 4.0}
	b.SizeToBuilding(0.025)

	if b.PowerMw != 2.0 || b.CapacityMwh != 4.0 {
		t.Errorf("a declared nameplate must be left alone, got %.3f MW / %.3f MWh", b.PowerMw, b.CapacityMwh)
	}
}

func TestPackDoesNotShrinkWhenTheBuildingIsQuiet(t *testing.T) {
	// A battery is an asset, not a reading. Once sized, a quiet hour does not shrink it —
	// a nameplate that tracked load would make the state-of-charge trace meaningless.
	b := Battery{}
	b.SizeToBuilding(1.5)
	sized := b.PowerMw
	b.SizeToBuilding(0.02)

	if b.PowerMw != sized {
		t.Errorf("pack shrank from %.4f to %.4f MW on a quiet observation", sized, b.PowerMw)
	}
}

func TestNoStorageUntilSomethingHasBeenObserved(t *testing.T) {
	// Before the engine knows what the building draws there is no basis for a pack, and
	// an unsized battery must not present itself as one.
	b := Battery{}
	b.SizeToBuilding(0)

	if b.Enabled {
		t.Error("a pack with no observed load behind it should not report itself as present")
	}
	b.Dispatch(60, 0.5, "peak")
	if b.DischargeMw != 0 {
		t.Errorf("a disabled pack must not dispatch, got %.4f MW", b.DischargeMw)
	}
}

func TestAPackTooSmallToBeABatteryIsNotDrawn(t *testing.T) {
	// A building drawing a few watts does not get a battery shaving milliwatts.
	b := Battery{}
	b.SizeToBuilding(1e-6)

	if b.Enabled {
		t.Error("a sub-threshold pack should be reported as absent rather than drawn")
	}
}
