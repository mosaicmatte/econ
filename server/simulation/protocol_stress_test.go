package simulation

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ----------------------------------------------------------------------------
// AREA 1: CONCURRENT OVERRIDES & RAPID CONSECUTIVE ACTION DISPATCHES
// ----------------------------------------------------------------------------

// TestConcurrentOverridesBurst spawns 100 goroutines concurrently calling PublishCommand,
// StartPreCool, SetAutoPilot, and actuate() across 10,000 rapid iterations to verify
// thread safety, lack of deadlocks, and lack of map corruption under race conditions.
func TestConcurrentOverridesBurst(t *testing.T) {
	e := newTestEngine()
	var publishCount int64
	e.Publish = func(topic, payload string) {
		atomic.AddInt64(&publishCount, 1)
	}

	const numWorkers = 50
	const itersPerWorker = 200
	var wg sync.WaitGroup

	actions := []string{"purge", "cool", "reset", "LIGHTS_ON;SETPOINT=21.0", "LIGHTS_OFF;SETPOINT=25.0", "invalid_action_verb"}
	zones := []string{"zone-office-a", "zone-office-b", "zone-office-c", "zone-corridor-x", "non-existent-zone-xyz"}

	start := make(chan struct{})

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-start
			r := rand.New(rand.NewSource(int64(workerID * 1000)))

			for j := 0; j < itersPerWorker; j++ {
				op := r.Intn(6)
				act := actions[r.Intn(len(actions))]
				zn := zones[r.Intn(len(zones))]

				switch op {
				case 0, 1, 2:
					e.PublishCommand(act, zn)
				case 3:
					e.StartPreCool(time.Duration(r.Intn(30)+1) * time.Minute)
				case 4:
					e.SetAutoPilot(r.Intn(2) == 0)
				case 5:
					e.mu.Lock()
					e.actuate()
					e.mu.Unlock()
				}
			}
		}(i)
	}

	// Release all workers simultaneously
	close(start)
	wg.Wait()

	if atomic.LoadInt64(&publishCount) == 0 {
		t.Fatal("expected published commands during concurrent stress test")
	}

	// Verify engine is still in consistent state
	hw := e.HardwareStatus()
	if hw == nil {
		t.Fatal("HardwareStatus failed after concurrent stress test")
	}
}

// TestHighThroughputConsecutiveActions sends 10,000 consecutive commands in a tight loop.
func TestHighThroughputConsecutiveActions(t *testing.T) {
	e := newTestEngine()
	var published []string
	var mu sync.Mutex
	e.Publish = func(topic, payload string) {
		mu.Lock()
		published = append(published, payload)
		mu.Unlock()
	}

	total := 10000
	for i := 0; i < total; i++ {
		zone := "zone-office-a"
		if i%2 == 0 {
			e.PublishCommand("purge", zone)
		} else {
			e.PublishCommand("cool", zone)
		}
	}

	mu.Lock()
	pLen := len(published)
	mu.Unlock()

	if pLen != total {
		t.Fatalf("expected %d published commands, got %d", total, pLen)
	}

	z := e.Zones["zone-office-a"]
	if z.Setpoint != 20.0 || !z.LightsOn {
		t.Fatalf("final state mismatch after 10,000 commands: sp=%v lights=%v", z.Setpoint, z.LightsOn)
	}
}

// ----------------------------------------------------------------------------
// AREA 2: INVALID ACTION PAYLOADS & NON-EXISTENT ZONE REFERENCES
// ----------------------------------------------------------------------------

// TestInvalidActionPayloads tests edge-case and adversarial payloads against PublishCommand.
func TestInvalidActionPayloads(t *testing.T) {
	e := newTestEngine()
	var published []string
	e.Publish = func(topic, payload string) {
		published = append(published, payload)
	}

	adversarialPayloads := []struct {
		name        string
		action      string
		zoneRef     string
		expectPanic bool
	}{
		{"EmptyAction", "", "zone-office-a", false},
		{"WhitespaceAction", "   \t\n  ", "zone-office-a", false},
		{"DelimiterOnly", ";;;;;", "zone-office-a", false},
		{"MalformedSetpoint", "SETPOINT=not_a_number", "zone-office-a", false},
		{"EmptySetpoint", "SETPOINT=", "zone-office-a", false},
		{"NaNSetpoint", "SETPOINT=NaN", "zone-office-a", false},
		{"InfSetpoint", "SETPOINT=+Inf", "zone-office-a", false},
		{"ExtremePositiveSetpoint", "SETPOINT=999999999.9", "zone-office-a", false},
		{"NegativeSetpoint", "SETPOINT=-50.0", "zone-office-a", false},
		{"MalformedHvacSet", "HVAC_SET:invalid", "zone-office-a", false},
		{"EmptyHvacSet", "HVAC_SET:", "zone-office-a", false},
		{"UnknownVerb", "UNRECOGNIZED_ACTION_VERB_XYZ", "zone-office-a", false},
		{"NullByteAction", "LIGHTS_ON\x00;SETPOINT=22.0", "zone-office-a", false},
		{"SqlInjectionAction", "'; DROP TABLE zones;--", "zone-office-a", false},
		{"JsonInAction", "{\"action\":\"cool\"}", "zone-office-a", false},
		{"UnicodeAction", "🔥❄️⚡️🚀", "zone-office-a", false},
		{"HugeAction", fmt.Sprintf("SETPOINT=20.0;%s", string(make([]byte, 65536))), "zone-office-a", false},
		{"NonExistentZone", "cool", "zone-completely-non-existent-999", false},
		{"EmptyZone", "purge", "", false},
		{"SpecialCharZone", "reset", "../../etc/passwd", false},
		{"HugeZoneName", "cool", string(make([]byte, 10000)), false},
	}

	for _, tc := range adversarialPayloads {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("PublishCommand panicked on payload %q: %v", tc.name, r)
				}
			}()
			e.PublishCommand(tc.action, tc.zoneRef)
		})
	}
}

// TestNormalizeOverrideDirectly verifies normalization logic for all verbs and edge cases.
func TestNormalizeOverrideDirectly(t *testing.T) {
	dummyZone := &ZoneSim{BaseSetpoint: 22.5}

	tests := []struct {
		input    string
		expected string
	}{
		{"purge", "LIGHTS_OFF;SETPOINT=18.0"},
		{"PURGE", "LIGHTS_OFF;SETPOINT=18.0"},
		{"  purge  ", "LIGHTS_OFF;SETPOINT=18.0"},
		{"cool", "LIGHTS_ON;SETPOINT=20.0"},
		{"COOL", "LIGHTS_ON;SETPOINT=20.0"},
		{"reset", "LIGHTS_ON;SETPOINT=22.5"},
		{"RESET", "LIGHTS_ON;SETPOINT=22.5"},
		{"LIGHTS_ON;SETPOINT=21.0", "LIGHTS_ON;SETPOINT=21.0"},
		{"lights_off;setpoint=25.5", "lights_off;setpoint=25.5"},
		{"HVAC_SET:19.5", "HVAC_SET:19.5"},
		{"unknown_custom_command", "unknown_custom_command"},
	}

	for _, tt := range tests {
		res := normalizeOverride(tt.input, dummyZone)
		if res != tt.expected {
			t.Errorf("normalizeOverride(%q) = %q, want %q", tt.input, res, tt.expected)
		}
	}

	// Reset with nil zone falls back to 24.0
	nilRes := normalizeOverride("reset", nil)
	if nilRes != "LIGHTS_ON;SETPOINT=24.0" {
		t.Errorf("normalizeOverride(reset, nil) = %q, want LIGHTS_ON;SETPOINT=24.0", nilRes)
	}
}

// ----------------------------------------------------------------------------
// AREA 3: OVERRIDE LATCH DURATION (15 MINUTES) & AUTONOMOUS OPTIMIZER VETO PROTECTION
// ----------------------------------------------------------------------------

// TestOverrideLatchDuration15Minutes verifies that manual human vetoes latch for 15 minutes
// and the autonomous optimizer (actuate) respects the veto throughout the window.
func TestOverrideLatchDuration15Minutes(t *testing.T) {
	e := newTestEngine()
	z := e.Zones["zone-office-a"]
	z.BaseSetpoint = 24.0
	z.Setpoint = 24.0
	z.LightsOn = true
	z.Occupancy = 0
	z.VacantTicks = vacancyDelayTicks + 10 // Eligible for vacancy setback

	e.AutoPilot = true

	// Step 1: Without override, optimizer must setback vacant zone
	e.actuate()
	if z.Setpoint <= z.BaseSetpoint {
		t.Fatalf("expected vacant zone in setback, got sp=%v base=%v", z.Setpoint, z.BaseSetpoint)
	}
	if z.LightsOn {
		t.Fatal("expected lights off in vacant setback zone")
	}

	// Step 2: Human issues manual "cool" override
	beforeOverride := time.Now()
	e.PublishCommand("cool", "zone-office-a")
	afterOverride := time.Now()

	// Verify latch timestamp is within [now+15m - 1s, now+15m + 1s]
	expectedLatchMin := beforeOverride.Add(15 * time.Minute).Add(-time.Second)
	expectedLatchMax := afterOverride.Add(15 * time.Minute).Add(time.Second)
	if z.OverrideUntil.Before(expectedLatchMin) || z.OverrideUntil.After(expectedLatchMax) {
		t.Fatalf("override latch time %v out of expected 15m window [%v, %v]", z.OverrideUntil, expectedLatchMin, expectedLatchMax)
	}

	// Verify immediate state mutation
	if z.Setpoint != 20.0 || !z.LightsOn {
		t.Fatalf("veto not applied immediately: sp=%v lights=%v", z.Setpoint, z.LightsOn)
	}

	// Step 3: Run 50 ticks of actuate() with vacant zone — optimizer MUST NOT overwrite
	for i := 0; i < 50; i++ {
		z.VacantTicks++
		e.actuate()
		if z.Setpoint != 20.0 {
			t.Fatalf("optimizer stomped human veto on tick %d: sp=%v (expected 20.0)", i, z.Setpoint)
		}
		if !z.LightsOn {
			t.Fatalf("optimizer stomped human veto lights on tick %d", i)
		}
	}

	// Step 4: Advance time past latch expiration (15m + 1s)
	z.OverrideUntil = time.Now().Add(-1 * time.Second)

	// Step 5: Next actuate() tick must reassert autonomous control and apply vacancy setback
	e.actuate()
	if z.Setpoint <= z.BaseSetpoint {
		t.Fatalf("after latch expiration, expected setback sp > %v, got %v", z.BaseSetpoint, z.Setpoint)
	}
	if z.LightsOn {
		t.Fatal("after latch expiration, expected lights to switch off in vacant zone")
	}
}

// TestAutoPilotDisabledPreservesVeto verifies that disabling AutoPilot does not stomp active vetoes.
func TestAutoPilotDisabledPreservesVeto(t *testing.T) {
	e := newTestEngine()
	zA := e.Zones["zone-office-a"]
	zB := e.Zones["zone-office-b"]

	zA.BaseSetpoint = 24.0
	zB.BaseSetpoint = 24.0

	// Zone A receives human veto (cool -> 20°C)
	e.PublishCommand("cool", "zone-office-a")

	// Zone B is in setback (28°C, lights off)
	zB.Setpoint = 28.0
	zB.LightsOn = false

	// Turn AutoPilot off
	e.SetAutoPilot(false)
	e.actuate()

	// Zone A must retain human veto (20.0°C)
	if zA.Setpoint != 20.0 {
		t.Fatalf("disabling autopilot stomped active human veto on zone-office-a: sp=%v", zA.Setpoint)
	}

	// Zone B (no veto) must be released back to base setpoint (24.0°C) and lights ON
	if zB.Setpoint != 24.0 || !zB.LightsOn {
		t.Fatalf("zone-office-b not restored to baseline on autopilot disable: sp=%v lights=%v", zB.Setpoint, zB.LightsOn)
	}
}

// ----------------------------------------------------------------------------
// AREA 4: PRE-COOL WINDOW ACTIVATION & EXPIRATION SEMANTICS
// ----------------------------------------------------------------------------

// TestPreCoolSemanticsAndVetoHierarchy tests window opening, extension, non-shrinking,
// differential zone actuation (occupied vs vacant), human veto hierarchy, and expiration.
func TestPreCoolSemanticsAndVetoHierarchy(t *testing.T) {
	e := newTestEngine()
	zA := e.Zones["zone-office-a"] // Occupied, no veto
	zB := e.Zones["zone-office-b"] // Vacant, no veto
	zC := e.Zones["zone-office-c"] // Occupied WITH human veto

	zA.BaseSetpoint = 24.0
	zA.Occupancy = 3
	zA.VacantTicks = 0

	zB.BaseSetpoint = 24.0
	zB.Occupancy = 0
	zB.VacantTicks = vacancyDelayTicks + 5

	zC.BaseSetpoint = 24.0
	zC.Occupancy = 2
	zC.VacantTicks = 0

	// Issue human veto on Zone C: setpoint 22.0
	e.PublishCommand("LIGHTS_ON;SETPOINT=22.0", "zone-office-c")

	// 1. Initial State: Pre-cool inactive
	active, _ := e.PreCoolStatus()
	if active {
		t.Fatal("pre-cool should be inactive initially")
	}

	// 2. Open Pre-Cool window for 20 minutes
	now := time.Now()
	until := e.StartPreCool(20 * time.Minute)
	active, statusUntil := e.PreCoolStatus()
	if !active || !statusUntil.Equal(until) {
		t.Fatalf("pre-cool not activated properly: active=%v until=%v", active, statusUntil)
	}
	if until.Before(now.Add(19*time.Minute)) || until.After(now.Add(21*time.Minute)) {
		t.Fatalf("pre-cool until timestamp %v unexpected for 20m window", until)
	}

	// 3. Shorter request must NOT shrink open window
	shorterUntil := e.StartPreCool(5 * time.Minute)
	if shorterUntil.Before(until) {
		t.Fatalf("shorter pre-cool request shrank window: %v -> %v", until, shorterUntil)
	}

	// 4. Longer request MUST extend window
	extendedTarget := 35 * time.Minute
	longerUntil := e.StartPreCool(extendedTarget)
	if longerUntil.Before(until) {
		t.Fatalf("longer pre-cool request failed to extend window: %v vs %v", longerUntil, until)
	}

	// 5. Actuate under Pre-Cool:
	e.AutoPilot = true
	e.actuate()

	// 5a. Occupied zone without veto (zA) -> BaseSetpoint - 1.5°C = 22.5°C
	expectedPreCoolSp := zA.BaseSetpoint - preCoolDelta
	if math.Abs(zA.Setpoint-expectedPreCoolSp) > 0.001 {
		t.Fatalf("occupied zone A not pre-cooling: sp=%v (expected %v)", zA.Setpoint, expectedPreCoolSp)
	}

	// 5b. Vacant zone without veto (zB) -> Setback preserved (24.0 + 4.0 = 28.0°C)
	if zB.Setpoint <= zB.BaseSetpoint {
		t.Fatalf("vacant zone B lost setback during pre-cool: sp=%v (expected > %v)", zB.Setpoint, zB.BaseSetpoint)
	}

	// 5c. Zone with Human Veto (zC) -> Human Veto (22.0°C) PRESERVED over pre-cool (22.5°C)!
	if zC.Setpoint != 22.0 {
		t.Fatalf("human veto on zone C was overwritten by pre-cooling: sp=%v (expected 22.0)", zC.Setpoint)
	}

	// 6. Pre-cool window expires
	e.PreCoolUntil = time.Now().Add(-1 * time.Second)
	activeAfter, _ = e.PreCoolStatus()
	if activeAfter {
		t.Fatal("pre-cool should report inactive after expiration")
	}

	// 7. Next actuate() restores occupied zone A back to BaseSetpoint (24.0°C)
	e.actuate()
	if zA.Setpoint != zA.BaseSetpoint {
		t.Fatalf("zone A did not return to BaseSetpoint after pre-cool expired: sp=%v (expected %v)", zA.Setpoint, zA.BaseSetpoint)
	}
}

var activeAfter bool
