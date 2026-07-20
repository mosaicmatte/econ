package simulation

import (
	"testing"
	"time"
)

// The arming window is the whole policy: work hours disarm the sweep, an empty window
// means always armed, an overnight window (night shift) inverts the day.
func TestPlugConfigArmed(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 7, 21, h, 30, 0, 0, time.Local) }

	day := PlugConfig{Enabled: true, WorkStartHour: 7, WorkEndHour: 19}
	if day.armed(at(12)) {
		t.Fatal("sweep must be disarmed during work hours")
	}
	if !day.armed(at(22)) || !day.armed(at(3)) {
		t.Fatal("sweep must be armed after hours and pre-dawn")
	}

	always := PlugConfig{Enabled: true, WorkStartHour: 0, WorkEndHour: 0}
	if !always.armed(at(12)) {
		t.Fatal("an empty work window means always armed")
	}

	night := PlugConfig{Enabled: true, WorkStartHour: 22, WorkEndHour: 6}
	if night.armed(at(23)) || night.armed(at(3)) {
		t.Fatal("overnight work window must disarm the sweep at night")
	}
	if !night.armed(at(12)) {
		t.Fatal("overnight work window must arm the sweep during the day")
	}

	off := PlugConfig{Enabled: false, WorkStartHour: 0, WorkEndHour: 0}
	if off.armed(at(23)) {
		t.Fatal("a disabled sweep is never armed")
	}
}

// A vacant non-critical zone sheds once armed and past grace; a critical zone never
// does; reoccupancy restores immediately; only shed time accumulates savings.
func TestPlugSweepShedAndRestore(t *testing.T) {
	e := newTestEngine()
	e.Zones["zone-server-1"] = &ZoneSim{Temp: 22, Type: "server-room", PlugStandbyW: 500}
	for _, z := range e.Zones {
		z.Occupancy = 0
		z.PlugStandbyW = 100
	}
	e.Zones["zone-server-1"].PlugStandbyW = 500
	e.SetPlugConfig(PlugConfig{Enabled: true, WorkStartHour: 0, WorkEndHour: 0, GraceMinutes: 0})

	var published []string
	e.Publish = func(topic, payload string) { published = append(published, topic+" "+payload) }
	e.Zones["zone-office-a"].MqttTopic = "esp32_a" // one zone has a real device listening

	now := time.Now()
	e.mu.Lock()
	e.lastPlugAt = now.Add(-time.Second)
	e.plugTick(now)
	e.mu.Unlock()

	if !e.Zones["zone-office-a"].PlugShed || !e.Zones["zone-corridor-x"].PlugShed {
		t.Fatal("vacant non-critical zones must shed when armed with zero grace")
	}
	if e.Zones["zone-server-1"].PlugShed {
		t.Fatal("critical zone types must never shed")
	}
	if len(published) != 1 || published[0] != "econ/commands/esp32_a PLUG_OFF" {
		t.Fatalf("exactly the hardware-bound zone gets the relay command, got %v", published)
	}
	// One second shed across the shed zones: 4 zones × 100 W × 0.7 switchable = 280 W·s.
	if saved := e.PlugSavedKwh(); saved < 270.0/3.6e6 || saved > 290.0/3.6e6 {
		t.Fatalf("savings must integrate switchable standby over wall time, got %.9f kWh", saved)
	}

	// Presence returns in the office: the sweep must restore it on the next tick.
	occ := 2
	e.IngestTelemetry("zone-office-a", "esp32_a", Measurement{Occupancy: &occ, Source: "esp32"})
	e.mu.Lock()
	e.plugTick(now.Add(2 * time.Second))
	e.mu.Unlock()
	if e.Zones["zone-office-a"].PlugShed {
		t.Fatal("reoccupied zone must restore")
	}
	if published[len(published)-1] != "econ/commands/esp32_a PLUG_ON" {
		t.Fatalf("restore must publish PLUG_ON, got %v", published)
	}
}

// Grace means grace: a zone that just emptied must not shed until the dwell elapses.
func TestPlugSweepGrace(t *testing.T) {
	e := newTestEngine()
	for _, z := range e.Zones {
		z.Occupancy = 0
		z.PlugStandbyW = 100
	}
	e.SetPlugConfig(PlugConfig{Enabled: true, WorkStartHour: 0, WorkEndHour: 0, GraceMinutes: 15})

	now := time.Now()
	e.mu.Lock()
	e.plugTick(now) // establishes PlugVacantSince = now
	e.mu.Unlock()
	if e.Zones["zone-office-a"].PlugShed {
		t.Fatal("must not shed inside the grace window")
	}

	e.mu.Lock()
	// Simulate the dwell having elapsed.
	for _, z := range e.Zones {
		z.PlugVacantSince = now.Add(-16 * time.Minute)
	}
	e.plugTick(now)
	e.mu.Unlock()
	if !e.Zones["zone-office-a"].PlugShed {
		t.Fatal("must shed once vacancy outlasts the grace window")
	}
}

// A live clamp reading beats the model, and goes stale like every other sensor.
func TestPlugMeasurementWinsAndExpires(t *testing.T) {
	e := newTestEngine()
	z := e.Zones["zone-office-a"]
	z.Occupancy = 2
	z.PlugStandbyW = 100

	if total, _ := z.plugNowW(); total != 100+2*plugActiveWPerOcc {
		t.Fatalf("model draw wrong: %v", total)
	}

	w := 512.0
	e.IngestTelemetry("zone-office-a", "esp32_a", Measurement{PlugW: &w, Source: "esp32"})
	if total, _ := z.plugNowW(); total != 512.0 {
		t.Fatalf("fresh clamp reading must win, got %v", total)
	}

	z.HwPlugAt = time.Now().Add(-hwStaleAfter - time.Second)
	if total, _ := z.plugNowW(); total != 100+2*plugActiveWPerOcc {
		t.Fatalf("stale clamp must fall back to the model, got %v", total)
	}
}
