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
}

func envF(key string, def float64) float64 {
	if s := os.Getenv(key); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	}
	return def
}

// NewBattery builds the BESS from env (site-configurable). Defaults describe a 2 MW / 4 MWh
// pack — a realistic size for a ~17 MW commercial campus. Set BESS_CAPACITY_MWH=0 for a
// building with no storage (nothing battery-related is then shown).
func NewBattery() Battery {
	capMwh := envF("BESS_CAPACITY_MWH", 4.0)
	return Battery{
		Enabled:     capMwh > 0,
		CapacityMwh: capMwh,
		PowerMw:     envF("BESS_POWER_MW", 2.0),
		Soc:         math.Max(0, math.Min(1, envF("BESS_INIT_SOC", 0.6))),
		accel:       envF("BESS_TIME_ACCEL", 1.0), // real-time SoC integration (~0.8%/min at 2MW/4MWh)
	}
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
