package simulation

import (
	"math"
	"os"
	"strconv"
	"time"
)

// Battery models a commercial Battery Energy Storage System (BESS). It arbitrages the EVN
// time-of-use tariff: it charges from the grid during cheap off-peak hours and discharges
// to offset building load during the expensive peak windows, cutting grid draw exactly when
// energy costs the most. State of charge integrates real power over time, bounded by the
// pack's usable capacity and its inverter power rating — the same limits a real EMS enforces.
// A site without storage simply runs with Enabled=false (BESS_CAPACITY_MWH=0).
type Battery struct {
	Enabled     bool
	CapacityMwh float64 // usable energy capacity
	PowerMw     float64 // inverter max charge/discharge rate
	Soc         float64 // state of charge, 0..1
	DischargeMw float64 // signed instantaneous power: + discharging to grid, - charging from grid
	Band        string  // current tariff band driving dispatch ("peak"/"offpeak"/"normal")
	accel       float64 // live-twin time acceleration so SoC visibly trends during a demo
	// Declared is true when the site stated its own nameplate in the environment. A
	// declared pack is a real asset and is never resized; an undeclared one is a modelled
	// pack the twin sizes to the building it is actually running (SizeToBuilding).
	Declared bool
}

// envFSet reports whether the variable was set at all, which is what separates "this site
// owns a 500 kW pack" from "this site has not said", and both from "this site has none".
func envFSet(key string) (float64, bool) {
	s := os.Getenv(key)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func envF(key string, def float64) float64 {
	if s := os.Getenv(key); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	}
	return def
}

// NewBattery builds the BESS.
//
// A site that owns a pack declares its nameplate: BESS_POWER_MW and BESS_CAPACITY_MWH are
// then taken literally and nothing rescales them. BESS_CAPACITY_MWH=0 says the building has
// no storage, and nothing battery-related is shown.
//
// With neither set, the twin models a pack — and the size of that pack has to follow the
// building. It used to be a flat 2 MW / 4 MWh, which is a fair description of storage on
// the ~17 MW commercial campus this engine was first written against and a fabrication on a
// 72 m2 house: eighty times the whole building's load, discharging 100% of it through every
// evening peak, so the dashboard reported a house running entirely off a battery it does not
// have and drawing nothing from the grid. The nameplate is left at zero here and filled in
// by SizeToBuilding once the engine knows what this building actually draws.
func NewBattery() Battery {
	capEnv, capSet := envFSet("BESS_CAPACITY_MWH")
	powEnv, powSet := envFSet("BESS_POWER_MW")
	b := Battery{
		Soc:   math.Max(0, math.Min(1, envF("BESS_INIT_SOC", 0.6))),
		accel: envF("BESS_TIME_ACCEL", 1.0), // real-time SoC integration
	}
	if capSet || powSet {
		b.Declared = true
		b.CapacityMwh = capEnv
		b.PowerMw = powEnv
		// Half a declaration is still a declaration: fill the unstated side from the
		// other at the library's duration rather than silently leaving it at zero.
		if capSet && !powSet && Storage().HoursAtRatedPower > 0 {
			b.PowerMw = capEnv / Storage().HoursAtRatedPower
		}
		if powSet && !capSet {
			b.CapacityMwh = powEnv * Storage().HoursAtRatedPower
		}
		b.Enabled = b.CapacityMwh > 0 && b.PowerMw > 0
	}
	return b
}

// SizeToBuilding gives an undeclared pack a nameplate proportional to the peak load this
// building has actually been observed at — the same principle as sizing the fan to the duct
// network it is attached to rather than to the building the code was written for.
//
// It only ever grows. A pack is a physical asset: it does not shrink because the building
// had a quiet hour, and a nameplate that moved with the load would make the state-of-charge
// trace meaningless. A site that declared its own nameplate is left alone.
func (b *Battery) SizeToBuilding(observedPeakMw float64) {
	if b.Declared || !(observedPeakMw > 0) {
		return
	}
	st := Storage()
	frac, hours := st.PowerFractionOfObservedPeak, st.HoursAtRatedPower
	if frac <= 0 || hours <= 0 {
		return
	}
	powerMw := observedPeakMw * frac
	if powerMw < st.MinPowerMw {
		// Too small to be a battery. Report no storage rather than draw one shaving watts.
		return
	}
	if powerMw <= b.PowerMw {
		return
	}
	b.PowerMw = powerMw
	b.CapacityMwh = powerMw * hours
	b.Enabled = true
}

// vnLoc pins TOU classification to Vietnam local time (ICT, UTC+7, no DST) so the engine's
// battery dispatch and the frontend's EVN tariff display always agree, regardless of the
// server's own timezone.
var vnLoc = time.FixedZone("ICT", 7*3600)

// touBand classifies a moment into an EVN tariff band, mirroring the frontend tariff.js
// schedule (Decision 963/QĐ-BCT, effective 22 Apr 2026): peak 17:30–22:30 Mon–Sat;
// off-peak 00:00–06:00 daily; normal otherwise (06:00–17:30 & 22:30–24:00).
func touBand(t time.Time) string {
	t = t.In(vnLoc)
	mins := t.Hour()*60 + t.Minute()
	if mins < 6*60 {
		return "offpeak" // 00:00–06:00 daily
	}
	if t.Weekday() != time.Sunday && mins >= 17*60+30 && mins < 22*60+30 {
		return "peak" // 17:30–22:30 Mon–Sat
	}
	return "normal"
}

// Dispatch advances the battery one step. dtSec is real elapsed seconds; loadMw is the site's
// current electrical load (discharge is capped at what the building actually draws). TOU
// arbitrage: discharge on peak, charge off-peak, hold on normal — with SoC and power limits.
func (b *Battery) Dispatch(dtSec, loadMw float64, band string) {
	b.Band = band
	if !b.Enabled || b.CapacityMwh <= 0 {
		b.DischargeMw = 0
		return
	}
	// TOU arbitrage: buy cheap overnight, spend through the pricier day. Charge on off-peak,
	// discharge hard through the peak windows, and trickle-discharge through normal daytime
	// hours (still dearer than off-peak) — the way a real EMS runs a commercial pack.
	target := 0.0
	switch band {
	case "offpeak":
		target = -b.PowerMw // charge from the cheap grid
	case "peak":
		target = b.PowerMw // full discharge to shave the expensive peak
	case "normal":
		target = b.PowerMw * 0.4 // modest daytime discharge
	}
	switch {
	case target > 0: // discharging
		target = math.Min(target, math.Max(0, loadMw)) // never export past the load
		if b.Soc <= 0.05 {
			target = 0 // depleted — stop discharging
		}
	case target < 0: // charging
		if b.Soc >= 0.98 {
			target = 0 // full — stop charging
		}
	}
	b.DischargeMw = target
	// Integrate SoC: energy (MWh) = power (MW) × hours; discharging lowers charge.
	hours := (dtSec * b.accel) / 3600.0
	b.Soc = math.Max(0, math.Min(1, b.Soc-(target*hours)/b.CapacityMwh))
}
