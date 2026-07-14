package simulation

import (
	"testing"
	"time"
)

// newTestEngine builds an engine with a tiny synthetic building. NewEngine is used so
// every internal map is initialized exactly as in production; the data file is absent
// under `go test`, which exercises the (intended) empty-building fallback.
func newTestEngine() *Engine {
	e := NewEngine()
	for _, id := range []string{"zone-office-a", "zone-office-b", "zone-office-c"} {
		e.Zones[id] = &ZoneSim{
			Temp: 24, WallTemp: 24, Type: "office",
			Setpoint: 24, BaseSetpoint: 24, Deadband: 1,
			CAir: 5e5, CWall: 4e6, RIn: 0.001, ROut: 0.0011,
			LightsOn: true,
		}
	}
	e.Zones["zone-corridor-x"] = &ZoneSim{Temp: 24, Type: "corridor"}
	return e
}

func fp(v float64) *float64 { return &v }
func ip(v int) *int         { return &v }

// Two physical boards with unknown identifiers must bind to two DIFFERENT office
// zones, and the binding must be sticky across messages.
func TestAssignDemoZoneDistinctAndSticky(t *testing.T) {
	e := newTestEngine()

	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{Occupancy: ip(2), Source: "pico"})
	e.IngestTelemetry("Level 4", "zone_1", Measurement{Occupancy: ip(3), Source: "esp32"})

	picoZone, esp32Zone := "", ""
	for id, z := range e.Zones {
		switch z.MqttTopic {
		case "pico_1":
			picoZone = id
		case "zone_1":
			esp32Zone = id
		}
	}
	if picoZone == "" || esp32Zone == "" {
		t.Fatalf("nodes not bound: pico=%q esp32=%q", picoZone, esp32Zone)
	}
	if picoZone == esp32Zone {
		t.Fatalf("both nodes bound to the same zone %q", picoZone)
	}
	if e.Zones[picoZone].Type != "office" || e.Zones[esp32Zone].Type != "office" {
		t.Fatalf("demo nodes must bind to office zones, got %q and %q",
			e.Zones[picoZone].Type, e.Zones[esp32Zone].Type)
	}

	// Sticky: the same identifier keeps resolving to the same zone.
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{Occupancy: ip(0), Source: "pico"})
	if e.Zones[picoZone].Occupancy != 0 {
		t.Fatalf("second message did not land on the sticky zone %q", picoZone)
	}
}

// A measured (tempReal) temperature must pull the zone's air temp to the sensor;
// a simulated placeholder temperature must never touch the pin.
func TestTempRealPinning(t *testing.T) {
	e := newTestEngine()

	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{
		Occupancy: ip(1), Temp: fp(30.0), Source: "pico", TempReal: true,
	})

	var z *ZoneSim
	for _, zz := range e.Zones {
		if zz.MqttTopic == "pico_1" {
			z = zz
		}
	}
	if z == nil {
		t.Fatal("pico not bound")
	}
	if !z.hwFresh() || z.HwTemp != 30.0 {
		t.Fatalf("expected fresh pin at 30.0, got fresh=%v temp=%v", z.hwFresh(), z.HwTemp)
	}

	// ~50 frames of the exponential pull (alpha 0.1) must converge from 24 to ~30.
	e.mu.Lock()
	for i := 0; i < 50; i++ {
		e.applyHardware()
	}
	e.mu.Unlock()
	if z.Temp < 29.5 {
		t.Fatalf("zone temp did not converge to the sensor: %v", z.Temp)
	}

	// A fake temperature (tempReal=false, e.g. ESP32 sim mode) must not move the pin.
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{
		Occupancy: ip(1), Temp: fp(10.0), Source: "pico", TempReal: false,
	})
	if z.HwTemp != 30.0 {
		t.Fatalf("simulated temp overwrote the pin: %v", z.HwTemp)
	}
}

// A stale pin (node unplugged) must release the zone back to the thermal model, and
// an explicit LWT "offline" must release it immediately.
func TestStalenessAndOfflineRelease(t *testing.T) {
	e := newTestEngine()
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{
		Occupancy: ip(1), Temp: fp(30.0), Source: "pico", TempReal: true,
	})
	var z *ZoneSim
	for _, zz := range e.Zones {
		if zz.MqttTopic == "pico_1" {
			z = zz
		}
	}

	z.HwTempAt = time.Now().Add(-hwStaleAfter - time.Second)
	if z.hwFresh() {
		t.Fatal("stale pin still reported fresh")
	}
	before := z.Temp
	e.mu.Lock()
	e.applyHardware()
	e.mu.Unlock()
	if z.Temp != before {
		t.Fatal("applyHardware moved a stale-pinned zone")
	}

	// Fresh again, then LWT offline: pin must drop instantly.
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{
		Occupancy: ip(1), Temp: fp(30.0), Source: "pico", TempReal: true,
	})
	if !z.hwFresh() {
		t.Fatal("expected fresh pin after re-ingest")
	}
	e.SetNodeStatus("pico_1", false)
	if z.hwFresh() || z.HwOnline {
		t.Fatalf("offline status did not release the pin: fresh=%v online=%v", z.hwFresh(), z.HwOnline)
	}

	// A reconnecting node boots in its firmware default state, so a status
	// transition must clear the command dedupe and force a re-send.
	var zoneId string
	for id, zz := range e.Zones {
		if zz.MqttTopic == "pico_1" {
			zoneId = id
		}
	}
	e.mu.Lock()
	e.lastCmd[zoneId] = "LIGHTS_OFF;SETPOINT=26.0"
	e.mu.Unlock()
	e.SetNodeStatus("pico_1", true)
	e.mu.Lock()
	_, still := e.lastCmd[zoneId]
	e.mu.Unlock()
	if still {
		t.Fatal("status transition did not clear the command dedupe entry")
	}
}

// The /api/hardware snapshot must list bound zones (sorted), with pin state.
func TestHardwareStatus(t *testing.T) {
	e := newTestEngine()
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{
		Occupancy: ip(2), Temp: fp(27.5), Source: "pico", TempReal: true,
	})
	e.IngestTelemetry("Level 4", "zone_1", Measurement{
		Occupancy: ip(3), Temp: fp(25.0), Source: "esp32", TempReal: false,
	})

	nodes := e.HardwareStatus()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 bound nodes, got %d", len(nodes))
	}
	for i := 1; i < len(nodes); i++ {
		if nodes[i-1].ZoneId > nodes[i].ZoneId {
			t.Fatal("snapshot not sorted by zoneId")
		}
	}
	byTopic := map[string]HardwareNode{}
	for _, n := range nodes {
		byTopic[n.Topic] = n
	}
	if n := byTopic["pico_1"]; !n.TempPinned || n.HwTemp != 27.5 || n.Source != "pico" || !n.Online {
		t.Fatalf("pico snapshot wrong: %+v", n)
	}
	if n := byTopic["zone_1"]; n.TempPinned {
		t.Fatalf("esp32 sim temp must not report a pin: %+v", n)
	}
}

// A manual veto must be mirrored onto the engine's own zone state immediately, so
// the 3D lighting and /api/hardware reflect the human command during the override
// latch rather than only after the optimizer reasserts control.
func TestPublishCommandAppliesState(t *testing.T) {
	e := newTestEngine()
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{Occupancy: ip(2), Source: "pico"})
	var z *ZoneSim
	for _, zz := range e.Zones {
		if zz.MqttTopic == "pico_1" {
			z = zz
		}
	}
	if z == nil {
		t.Fatal("pico not bound")
	}

	z.LightsOn = true
	e.PublishCommand("LIGHTS_OFF;SETPOINT=27.5", "Pico Lab")
	if z.LightsOn {
		t.Fatal("veto did not switch lights off engine-side")
	}
	if z.Setpoint != 27.5 {
		t.Fatalf("veto setpoint not applied: %v", z.Setpoint)
	}
	if !time.Now().Before(z.OverrideUntil) {
		t.Fatal("override latch not set")
	}

	// High-level verb path: "reset" normalizes to LIGHTS_ON;SETPOINT=<base>.
	e.PublishCommand("reset", "Pico Lab")
	if !z.LightsOn || z.Setpoint != z.BaseSetpoint {
		t.Fatalf("reset verb not applied: lights=%v sp=%v", z.LightsOn, z.Setpoint)
	}
}

// Physics-grounded AFDD: the first real measurement seeds the shadow model, a healthy
// room (measurement tracking the model) keeps the residual near zero, and a sustained
// divergence must push the smoothed residual over the alert threshold — surfaced via
// the /api/hardware snapshot. No training data involved anywhere.
func TestShadowModelAfddResidual(t *testing.T) {
	e := newTestEngine()
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{
		Occupancy: ip(1), Temp: fp(24.0), Source: "pico", TempReal: true,
	})
	var z *ZoneSim
	for _, zz := range e.Zones {
		if zz.MqttTopic == "pico_1" {
			z = zz
		}
	}
	if z == nil {
		t.Fatal("pico not bound")
	}
	if z.ShadowTemp != 24.0 {
		t.Fatalf("first real temp must seed the shadow model, got %v", z.ShadowTemp)
	}

	// Healthy: measurement equals the model — the residual must stay near zero.
	e.mu.Lock()
	for i := 0; i < 200; i++ {
		e.applyHardware()
	}
	e.mu.Unlock()
	if z.ResidualEma > 0.5 {
		t.Fatalf("healthy zone accumulated a residual: %v", z.ResidualEma)
	}

	// Fault: the room measures 6°C hotter than the physics says it should be
	// (e.g. a blocked coil). The shadow seed must NOT re-anchor to the new reading.
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{
		Occupancy: ip(1), Temp: fp(30.0), Source: "pico", TempReal: true,
	})
	if z.ShadowTemp != 24.0 {
		t.Fatalf("shadow model re-seeded on a later measurement: %v", z.ShadowTemp)
	}
	e.mu.Lock()
	for i := 0; i < 400; i++ {
		e.applyHardware()
	}
	e.mu.Unlock()
	if z.ResidualEma < afddThreshold {
		t.Fatalf("sustained 6°C divergence did not cross the AFDD threshold: %v", z.ResidualEma)
	}

	nodes := e.HardwareStatus()
	var n HardwareNode
	for _, nn := range nodes {
		if nn.Topic == "pico_1" {
			n = nn
		}
	}
	if !n.AfddAlert || n.Residual < afddThreshold || n.ShadowTemp != 24.0 {
		t.Fatalf("AFDD alert not surfaced in snapshot: %+v", n)
	}
}

// A pre-cool window must drive occupied live zones below their base setpoint, leave
// vacant zones in setback, and release cleanly when the window closes.
func TestPreCoolWindow(t *testing.T) {
	e := newTestEngine()
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{Occupancy: ip(3), Source: "pico"})
	var z *ZoneSim
	for _, zz := range e.Zones {
		if zz.MqttTopic == "pico_1" {
			z = zz
		}
	}
	if z == nil {
		t.Fatal("pico not bound")
	}

	if active, _ := e.PreCoolStatus(); active {
		t.Fatal("pre-cool must start inactive")
	}
	first := e.StartPreCool(10 * time.Minute)
	if active, until := e.PreCoolStatus(); !active || !until.Equal(first) {
		t.Fatalf("window not open: active=%v until=%v", active, until)
	}
	// A shorter overlapping request must never shrink an open window.
	if second := e.StartPreCool(1 * time.Minute); second.Before(first) {
		t.Fatalf("window shrank: %v -> %v", first, second)
	}

	e.mu.Lock()
	e.actuate()
	e.mu.Unlock()
	if z.Setpoint != z.BaseSetpoint-preCoolDelta {
		t.Fatalf("occupied zone not pre-cooling: sp=%v base=%v", z.Setpoint, z.BaseSetpoint)
	}

	// Vacant zones must keep the energy-saving setback even during pre-cool.
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{Occupancy: ip(0), Source: "pico"})
	e.mu.Lock()
	z.VacantTicks = vacancyDelayTicks
	e.actuate()
	vacantSp := z.Setpoint
	e.mu.Unlock()
	if vacantSp != z.BaseSetpoint+4.0 {
		t.Fatalf("vacant zone lost its setback during pre-cool: %v", vacantSp)
	}

	// Window closes: the optimizer must return the (reoccupied) zone to base setpoint.
	e.IngestTelemetry("Pico Lab", "pico_1", Measurement{Occupancy: ip(3), Source: "pico"})
	e.mu.Lock()
	e.PreCoolUntil = time.Now().Add(-time.Second)
	e.actuate()
	e.mu.Unlock()
	if z.Setpoint != z.BaseSetpoint {
		t.Fatalf("zone did not release to base setpoint after the window: %v", z.Setpoint)
	}
}

// The original CV entry point must keep working and must not pin temperature.
func TestSetZoneOccupancyCompat(t *testing.T) {
	e := newTestEngine()
	e.SetZoneOccupancy("Level 4", "zone_1", 5)

	var z *ZoneSim
	for _, zz := range e.Zones {
		if zz.MqttTopic == "zone_1" {
			z = zz
		}
	}
	if z == nil {
		t.Fatal("cv node not bound")
	}
	if !z.Live || z.Occupancy != 5 {
		t.Fatalf("occupancy ingestion broken: live=%v occ=%d", z.Live, z.Occupancy)
	}
	if z.HwSource != "cv" || z.hwFresh() {
		t.Fatalf("cv ingestion must attribute source=cv and never pin: src=%q fresh=%v",
			z.HwSource, z.hwFresh())
	}
}
