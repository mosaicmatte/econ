package simulation

// Automated Plug Load Control (APLC).
//
// Plug loads — desktops, monitors, printers, kettles, chargers — are the end use a
// conventional BMS neither meters nor controls: it manages chillers, cooling towers,
// lighting, pumps, fans and elevators, and stops at the wall socket. In the Hanoi
// office-tower case study this project benchmarks against (Luong et al. 2025, JOMC
// 15(2), doi:10.54772/jomc.v15i02.1190) that blind spot made plug loads the single
// LARGEST end use — 26.4% of energy — in a 117,000 m² building that already ran a
// full BMS. (The same paper reports 35.3% of operational CO2 for plug loads, but its
// energy and carbon shares disagree while it applies a single grid emission factor,
// under which they must be equal; only the energy share is used here. See EVIDENCE.md.)
// The 57-building Vietnam survey (Hoang et al.
// 2022, doi:10.55066/proc-icec.2021.19) puts appliance intensity at 17.7–20 kWh/m²·yr,
// second only to air conditioning.
//
// The twin already owns the two capabilities that blind spot is made of:
//
//	sensing  — per-zone occupancy (CV / mmWave / PIR over MQTT), per-zone geometry
//	control  — per-zone edge actuation (ESP32 relays on econ/commands/<topic>)
//
// So the APLC closes the loop the BMS could not: model (or measure, via an SCT-013
// clamp) each zone's plug draw, and after hours sweep the switchable portion off in
// zones that are verifiably empty — restoring the instant presence returns. Critical
// zone types (server rooms, mechanical) are never swept.
//
import (
	"math"
	"sort"
	"time"
)

// Model constants. Per-occupant active load: ~130 W connected (laptop + dual monitors
// + dock, plus a share of printers/pantry/network edge) × a 0.5 coincidence factor —
// the k_i in the survey's own end-use formula (Hoang et al. 2022, eq. 4): not every
// device draws at once. For a typical 10 m²/person office this annualizes to
// ~20 kWh/m²·yr active + ~9 kWh/m²·yr standby ≈ the 17.7–20 the survey measured and
// the ~29 the 2025 case study's 26.4% share implies.
const (
	plugActiveWPerOcc  = 65.0 // W per present occupant (coincidence-weighted average draw)
	plugStandbyWPerM2  = 1.2  // W/m² of always-on phantom load (idle equipment, standby)
	plugSwitchableFrac = 0.7   // portion of standby on sweepable circuits; the rest
	// (network gear, fridges, anything on the critical way) stays energized
	plugDefaultAreaM2 = 25.0 // fallback when a zone has no usable polygon
)

// PlugConfig is the sweep policy. Work hours are the local-time window [WorkStartHour,
// WorkEndHour) during which the sweep is disarmed; equal start and end means no work
// hours at all (the sweep is always armed — useful for tests and 24/7 sites are the
// opposite: Enabled=false). CriticalTypes are zone types never swept regardless.
type PlugConfig struct {
	Enabled       bool     `json:"enabled"`
	WorkStartHour int      `json:"workStartHour"`
	WorkEndHour   int      `json:"workEndHour"`
	GraceMinutes  int      `json:"graceMinutes"`
	CriticalTypes []string `json:"criticalTypes"`
}

func defaultPlugConfig() PlugConfig {
	return PlugConfig{
		Enabled:       true,
		WorkStartHour: 7,
		WorkEndHour:   19,
		GraceMinutes:  15,
		CriticalTypes: []string{"server-room", "mechanical"},
	}
}

// armed reports whether the sweep may act right now: enabled and outside work hours.
func (c PlugConfig) armed(now time.Time) bool {
	if !c.Enabled {
		return false
	}
	h := now.Hour()
	if c.WorkStartHour == c.WorkEndHour {
		return true // empty work window: always after hours
	}
	if c.WorkStartHour < c.WorkEndHour {
		return h < c.WorkStartHour || h >= c.WorkEndHour
	}
	return h >= c.WorkEndHour && h < c.WorkStartHour // overnight work window
}

func (c PlugConfig) critical(zoneType string) bool {
	for _, t := range c.CriticalTypes {
		if t == zoneType {
			return true
		}
	}
	return false
}

// plugFresh reports whether a physical power meter (SCT-013 clamp on an edge node) is
// measuring this zone's plug circuit right now. Same freshness discipline as every
// other sensor: absent is absent, never zero, never last-known-forever.
func (z *ZoneSim) plugFresh() bool {
	return !z.HwPlugAt.IsZero() && time.Since(z.HwPlugAt) < hwStaleAfter
}

// plugNowW returns the zone's current plug draw and the standby component of it.
// A live clamp measurement wins outright; otherwise the model: always-on standby
// (reduced to the non-switchable floor while shed) plus per-occupant active load.
func (z *ZoneSim) plugNowW() (total, standby float64) {
	standby = z.PlugStandbyW
	if z.PlugShed {
		standby = z.PlugStandbyW * (1 - plugSwitchableFrac)
	}
	total = standby + float64(z.Occupancy)*plugActiveWPerOcc
	if z.plugFresh() {
		// Measured circuits report what the clamp reads; the standby attribution is
		// capped by the measurement so the split can never exceed reality.
		return z.HwPlugW, math.Min(z.HwPlugW, standby)
	}
	return total, standby
}

// plugTick runs the sweep for one engine tick. Called from Start's loop with e.mu
// held. Savings integrate on wall-clock time (the sim's dt accelerates; avoided
// kilowatt-hours must not).
func (e *Engine) plugTick(now time.Time) {
	dt := now.Sub(e.lastPlugAt).Seconds()
	e.lastPlugAt = now
	if dt < 0 || dt > 10 {
		dt = 0 // first tick or a clock jump: never integrate a bogus interval
	}
	armed := e.Plug.armed(now)
	grace := time.Duration(e.Plug.GraceMinutes) * time.Minute

	for _, z := range e.Zones {
		// Vacancy dwell: real occupancy for instrumented zones, the sim's for the rest.
		if z.Occupancy > 0 {
			z.PlugVacantSince = time.Time{}
		} else if z.PlugVacantSince.IsZero() {
			z.PlugVacantSince = now
		}

		shouldShed := armed &&
			!e.Plug.critical(z.Type) &&
			!z.PlugVacantSince.IsZero() &&
			now.Sub(z.PlugVacantSince) >= grace

		if shouldShed != z.PlugShed {
			z.PlugShed = shouldShed
			// Actuate only where a real device is listening. Simulated zones shed in
			// the model alone; a zone bound to an edge node gets the relay command,
			// and reoccupancy restores it before the returning occupant sits down.
			if z.MqttTopic != "" && e.Publish != nil {
				cmd := "PLUG_ON"
				if shouldShed {
					cmd = "PLUG_OFF"
				}
				e.Publish("econ/commands/"+z.MqttTopic, cmd)
			}
		}

		if z.PlugShed {
			// Conservative accounting: only the switchable standby the sweep actually
			// turned off, never the active load (which vacancy removed by itself).
			e.plugSavedKwh += z.PlugStandbyW * plugSwitchableFrac * dt / 3.6e6
		}
	}
}

// PlugZone is one row of the phantom-load table: where the always-on watts live.
type PlugZone struct {
	ZoneId   string  `json:"zoneId"`
	Type     string  `json:"type"`
	StandbyW float64 `json:"standbyW"`
	Shed     bool    `json:"shed"`
	Critical bool    `json:"critical"`
	Measured bool    `json:"measured"` // a live clamp is metering this zone
}

// PlugStatus is the /api/plugs snapshot.
type PlugStatus struct {
	Config       PlugConfig `json:"config"`
	Armed        bool       `json:"armed"`
	TotalKw      float64    `json:"totalKw"`
	StandbyKw    float64    `json:"standbyKw"`
	ShedKw       float64    `json:"shedKw"`
	ShedZones    int        `json:"shedZones"`
	MeteredZones int        `json:"meteredZones"`
	SavedKwh     float64    `json:"savedKwh"`
	TopStandby   []PlugZone `json:"topStandby"`
}

// PlugSnapshot reports the live plug-load picture: totals, sweep state, cumulative
// savings, and the topN zones by always-on standby (the phantom-load leaderboard a
// facility manager works through with a power strip and a timer).
func (e *Engine) PlugSnapshot(topN int) PlugStatus {
	e.mu.Lock()
	defer e.mu.Unlock()

	s := PlugStatus{Config: e.Plug, Armed: e.Plug.armed(time.Now()), SavedKwh: e.plugSavedKwh}
	rows := make([]PlugZone, 0, len(e.Zones))
	for id, z := range e.Zones {
		total, standby := z.plugNowW()
		s.TotalKw += total / 1000
		s.StandbyKw += standby / 1000
		if z.PlugShed {
			s.ShedZones++
			s.ShedKw += z.PlugStandbyW * plugSwitchableFrac / 1000
		}
		measured := z.plugFresh()
		if measured {
			s.MeteredZones++
		}
		rows = append(rows, PlugZone{
			ZoneId: id, Type: z.Type, StandbyW: z.PlugStandbyW,
			Shed: z.PlugShed, Critical: e.Plug.critical(z.Type), Measured: measured,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].StandbyW != rows[j].StandbyW {
			return rows[i].StandbyW > rows[j].StandbyW
		}
		return rows[i].ZoneId < rows[j].ZoneId
	})
	if topN > 0 && len(rows) > topN {
		rows = rows[:topN]
	}
	s.TopStandby = rows
	return s
}

// SetPlugConfig validates, clamps and applies a new sweep policy, returning the
// effective config. Zones that no longer qualify restore on the next tick.
func (e *Engine) SetPlugConfig(c PlugConfig) PlugConfig {
	clampHour := func(h int) int {
		if h < 0 {
			return 0
		}
		if h > 23 {
			return 23
		}
		return h
	}
	c.WorkStartHour = clampHour(c.WorkStartHour)
	c.WorkEndHour = clampHour(c.WorkEndHour)
	if c.GraceMinutes < 0 {
		c.GraceMinutes = 0
	}
	if c.GraceMinutes > 240 {
		c.GraceMinutes = 240
	}
	if c.CriticalTypes == nil {
		c.CriticalTypes = defaultPlugConfig().CriticalTypes
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Plug = c
	return c
}

// PlugSavedKwh / RestorePlugSavedKwh let main.go persist the cumulative savings
// counter across restarts (data/ rides a named volume; an avoided-energy figure that
// zeroes on every deploy is not a number anyone can report).
func (e *Engine) PlugSavedKwh() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.plugSavedKwh
}

func (e *Engine) RestorePlugSavedKwh(kwh float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if kwh > e.plugSavedKwh {
		e.plugSavedKwh = kwh
	}
}

// polygonAreaM2 is the shoelace area of a zone polygon (metres in, m² out).
func polygonAreaM2(poly [][]float64) float64 {
	if len(poly) < 3 {
		return 0
	}
	a := 0.0
	for i := range poly {
		p, q := poly[i], poly[(i+1)%len(poly)]
		if len(p) < 2 || len(q) < 2 {
			return 0
		}
		a += p[0]*q[1] - q[0]*p[1]
	}
	return math.Abs(a) / 2
}
