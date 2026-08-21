package main

import (
	"testing"

	"econ/schema/Telemetry"

	flatbuffers "github.com/google/flatbuffers/go"
)

// Round-trips the hand-generated accessors for the appended fields. flatc was not
// available, so the vtable slot numbers and offsets in ZoneData.go / GlobalData.go were
// written by hand; an off-by-one there would silently read a neighbouring field.
func TestAppendedFieldsRoundTrip(t *testing.T) {
	b := flatbuffers.NewBuilder(256)
	id := b.CreateString("zone-x")
	Telemetry.ZoneDataStart(b)
	Telemetry.ZoneDataAddId(b, id)
	Telemetry.ZoneDataAddTemp(b, 25.5)
	Telemetry.ZoneDataAddOccupants(b, 7)
	Telemetry.ZoneDataAddLoad(b, 1.25)
	Telemetry.ZoneDataAddLightsOn(b, false)
	Telemetry.ZoneDataAddHumidity(b, 61.5)
	Telemetry.ZoneDataAddCo2(b, 842)
	Telemetry.ZoneDataAddPlugW(b, 310)
	Telemetry.ZoneDataAddPlugShed(b, true)
	Telemetry.ZoneDataAddSupplyC(b, 9.5)
	Telemetry.ZoneDataAddSupplyReal(b, true)
	off := Telemetry.ZoneDataEnd(b)
	b.Finish(off)

	z := Telemetry.GetRootAsZoneData(b.FinishedBytes(), 0)
	for _, c := range []struct {
		name string
		got  float32
		want float32
	}{
		{"temp", z.Temp(), 25.5},
		{"load", z.Load(), 1.25},
		{"humidity", z.Humidity(), 61.5},
		{"co2", z.Co2(), 842},
		{"plugW", z.PlugW(), 310},
		{"supplyC", z.SupplyC(), 9.5},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if z.Occupants() != 7 {
		t.Errorf("occupants = %d, want 7", z.Occupants())
	}
	if z.LightsOn() {
		t.Error("lightsOn = true, want false")
	}
	if !z.PlugShed() {
		t.Error("plugShed = false, want true")
	}
	if !z.SupplyReal() {
		t.Error("supplyReal = false, want true")
	}

	// A ZoneData written WITHOUT the appended fields must still decode, reading the
	// schema defaults — this is what keeps an old engine and a new dashboard compatible.
	b2 := flatbuffers.NewBuilder(128)
	id2 := b2.CreateString("zone-old")
	Telemetry.ZoneDataStart(b2)
	Telemetry.ZoneDataAddId(b2, id2)
	Telemetry.ZoneDataAddTemp(b2, 22.0)
	off2 := Telemetry.ZoneDataEnd(b2)
	b2.Finish(off2)
	old := Telemetry.GetRootAsZoneData(b2.FinishedBytes(), 0)
	if old.SupplyC() != 0 || old.SupplyReal() {
		t.Errorf("absent appended fields must default: supplyC=%v supplyReal=%v", old.SupplyC(), old.SupplyReal())
	}
	if !old.LightsOn() {
		t.Error("lightsOn default must stay true")
	}

	// GlobalData: same check for the appended AHU pressure.
	b3 := flatbuffers.NewBuilder(256)
	Telemetry.GlobalDataStart(b3)
	Telemetry.GlobalDataAddBuildingLoadMw(b3, 1.5)
	Telemetry.GlobalDataAddZonesInSetback(b3, 4)
	Telemetry.GlobalDataAddAutoPilot(b3, false)
	Telemetry.GlobalDataAddAhuPressurePa(b3, 512.25)
	off3 := Telemetry.GlobalDataEnd(b3)
	b3.Finish(off3)
	g := Telemetry.GetRootAsGlobalData(b3.FinishedBytes(), 0)
	if g.AhuPressurePa() != 512.25 {
		t.Errorf("ahuPressurePa = %v, want 512.25", g.AhuPressurePa())
	}
	if g.BuildingLoadMw() != 1.5 || g.ZonesInSetback() != 4 || g.AutoPilot() {
		t.Errorf("appending ahuPressurePa disturbed existing fields: load=%v setback=%d autopilot=%v",
			g.BuildingLoadMw(), g.ZonesInSetback(), g.AutoPilot())
	}
}
