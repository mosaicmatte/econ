package simulation

import "testing"

// With Auto-Pilot on, a vacant zone past the dwell is set back and counted; with it off,
// the optimizer is suspended entirely and touches nothing.
func TestAutoPilotGatesSetback(t *testing.T) {
	e := newTestEngine()
	for _, z := range e.Zones {
		z.Occupancy = 0
		z.VacantTicks = vacancyDelayTicks // already past the safety dwell
		z.BaseSetpoint = 24
		z.Setpoint = 24
	}

	e.AutoPilot = true
	e.actuate()
	if e.zonesInSetback == 0 {
		t.Fatal("auto-pilot on: vacant zones must be set back and counted")
	}
	any := false
	for _, z := range e.Zones {
		if z.Setpoint > z.BaseSetpoint+0.01 {
			any = true
		}
	}
	if !any {
		t.Fatal("auto-pilot on: at least one zone should hold a raised (setback) setpoint")
	}

	// Reset, then confirm OFF suspends the optimizer completely.
	for _, z := range e.Zones {
		z.Setpoint = 24
		z.LightsOn = true
	}
	e.AutoPilot = false
	e.actuate()
	if e.zonesInSetback != 0 {
		t.Fatalf("auto-pilot off: nothing should be in setback, got %d", e.zonesInSetback)
	}
	for id, z := range e.Zones {
		if z.Setpoint != 24 {
			t.Fatalf("auto-pilot off: setpoints must hold, zone %s moved to %.1f", id, z.Setpoint)
		}
	}
}

// A pure-sim zone (no MqttTopic) is set back in the model but must NOT emit an MQTT
// command; a hardware-bound zone must.
func TestAutoPilotPublishesOnlyToHardware(t *testing.T) {
	e := newTestEngine()
	var published []string
	e.Publish = func(topic, payload string) { published = append(published, topic) }
	e.Zones["zone-office-a"].MqttTopic = "esp32_a" // one real device
	for _, z := range e.Zones {
		z.Occupancy = 0
		z.VacantTicks = vacancyDelayTicks
	}
	e.AutoPilot = true
	e.actuate()

	if len(published) != 1 || published[0] != "econ/commands/esp32_a" {
		t.Fatalf("only the hardware-bound zone should be commanded, got %v", published)
	}
	// But the sim zones are still in setback in the model.
	if e.Zones["zone-office-b"].Setpoint <= e.Zones["zone-office-b"].BaseSetpoint {
		t.Fatal("sim zone should still be set back in the engine even without an MQTT command")
	}
}
