package simulation

// Learned per-room dynamics — the model that gives the twin real room awareness.
//
// baselines.go learns what a room's readings usually ARE. That is enough to say "this is
// abnormal", but not to say what the room is about to DO. This file learns the second
// thing: each room's own physical response, identified online from its telemetry.
//
// For every zone we fit two first-order energy/mass balances by recursive least squares:
//
//	thermal:  dT/dt = θ0·(T_out − T_in) + θ1·flow·(T_in − T_supply) + θ2·occupancy + θ3
//	co2:      dC/dt = φ0·occupancy      + φ1·(C_out − C)            + φ2
//
// Those are not curve-fitting conveniences, they are the actual lumped physics, so each
// fitted coefficient is a physical property of that specific room, in engineering units:
//
//	θ0 → envelope coupling; 1/θ0 is the room's thermal time constant (how long it holds heat)
//	θ1 → cooling authority: how much heat this room's VAV actually removes per unit of flow
//	θ2 → heat gain per occupant, as this room really experiences it
//	φ1 → the room's air-change rate (ACH) — its measured ventilation, not its design value
//
// A room whose learned cooling authority is collapsing is a starved or failing VAV. A room
// whose learned ACH cannot keep up with its learned per-occupant CO2 generation is
// under-ventilated for the way it is actually used. Neither judgement is a threshold
// someone typed in; both fall out of the room's own measured behaviour.
//
// Because the models are first-order and linear, they also integrate forward in closed
// form, which is what turns the panel from reactive to predictive: holding the current
// drivers, the room settles at a computable equilibrium and approaches it exponentially,
// so "this room reaches its limit in ~14 min" is arithmetic on learned constants rather
// than a guess.
//
// Honesty discipline, same as the baselines: a fit is never trusted until it has seen
// enough observations AND its coefficients are physically sane (positive envelope
// coupling, cooling that actually cools, a positive air-change rate). A room that has sat
// perfectly still teaches nothing about its own time constant, and this model says so
// instead of inventing one.
//
// Time base: observations are timestamped on the SIMULATION clock, which the engine
// accelerates during faults and recovery (engine.go Start). Learning on the same clock the
// physics advances on keeps every learned constant in one consistent timescale; using
// wall-clock would make a room's time constant appear to change purely because the demo
// sped up.

import (
	"encoding/json"
	"math"
	"sort"
	"sync"
)

const (
	// dynamicsSampleSimSecs is the spacing between observations, in simulation seconds.
	//
	// This interval is set by NOISE, not by responsiveness. The model learns from finite
	// differences, and differencing amplifies sensor noise by 1/Δt: an NDIR sensor good to
	// ±8 ppm, differenced over 30 s, produces a derivative uncertain by ~1350 ppm/h against
	// a true signal of order 100 ppm/h — the "measurement" is then almost entirely noise,
	// and least squares fits the noise. The same arithmetic ruins temperature: ±0.1 °C over
	// 30 s is ±17 °C/h against a true rate of a few °C/h.
	//
	// Five minutes (matching the engine's own history cadence) cuts that amplification
	// tenfold and is still short relative to any real room's thermal time constant, so no
	// dynamics of interest are lost. Pairing the derivative with a MIDPOINT regressor (see
	// observeRoom) removes the residual errors-in-variables bias.
	dynamicsSampleSimSecs = 300.0
	// NOTE: an EMA pre-filter was tried here and removed. At this sampling interval its lag
	// is comparable to the CO2 time constant itself, so it distorted the dynamics being
	// identified (measured air-change rate biased from 2.0 down to 0.9 ACH). You cannot
	// filter aggressively relative to the response you are trying to measure; the longer
	// interval above is what buys the noise reduction, without the phase error.
	// dynamicsMaxGapSim discards a finite difference taken across a gap this long (a
	// pause, a reload, a restored snapshot) — the two endpoints are not one trajectory.
	dynamicsMaxGapSim = 3000.0
	// dynamicsMature is how many observations a fit needs before it may drive a
	// recommendation. Higher than the baselines' bar: identifying a response needs the room
	// to have actually moved, not just to have been watched.
	//
	// At the 5-minute sampling interval above this is ~3 hours of building time, which is
	// simply what identifying a room's thermal response honestly costs — a room whose time
	// constant is measured in hours cannot be characterised in minutes. 36 samples against
	// 4 thermal parameters is a 9:1 ratio, enough for a stable fit without pretending to
	// certainty. The model reports "still learning" until then rather than predicting from
	// a fit it has not earned.
	dynamicsMature = 36
	// dynamicsLambda is the RLS forgetting factor. 0.999 gives an effective memory of
	// ~1000 observations, so the fit tracks genuine drift (a fouling coil, a failing
	// damper) while ignoring single-sample noise.
	dynamicsLambda = 0.999
	// dynamicsP0 seeds the covariance: large = "I know nothing yet", so early
	// observations move the estimate freely before it settles.
	dynamicsP0 = 50.0
	// supplyAirC is the AHU discharge temperature the cooling regressor is referenced to,
	// matching the engine's own cooling law (engine.go tick). Cooling power scales with
	// flow × (room − supply), the physical heat-extraction term.
	supplyAirC = 12.0
	// outdoorCo2Ppm is the outdoor concentration a room decays toward with ventilation.
	outdoorCo2Ppm = 400.0
	// dynamicsResAlphaFloor bounds the residual EMA's adaptivity, mirroring the baseline
	// model's floor so the reported fit quality keeps tracking recent behaviour.
	dynamicsResAlphaFloor = 0.05
	// --- covariance windup protection ---
	//
	// Exponential forgetting divides the covariance by λ on EVERY update, including
	// updates that carry no new information. A building that settles into steady state
	// supplies exactly those: the drivers stop moving, the regressors go nearly constant,
	// and there is nothing left to identify. Left alone, P then grows without bound, the
	// estimator becomes arbitrarily sensitive to noise, and a model that was correct for
	// hours silently decays into nonsense. (Observed for real: a five-hour soak took 1152
	// identified rooms down to 0.)
	//
	// The standard remedy is to forget only when the data justifies it. dynamicsMinInfo is
	// the information a sample must carry (xᵀPx) before forgetting is applied at all;
	// below it the update runs with λ = 1, so a quiet building holds its identification
	// steady instead of eroding it.
	dynamicsMinInfo = 1e-3
	// dynamicsMaxTraceP is a hard backstop on the covariance size, in case a pathological
	// sequence inflates P despite the gate above. Rescaling preserves the SHAPE of the
	// covariance (relative confidence between coefficients) while bounding its magnitude.
	dynamicsMaxTraceP = 1e4

	// --- physical feasibility bounds ---
	//
	// A building under closed-loop control is a poor subject for system identification. The
	// optimizer drives airflow as a function of temperature error, so the cooling regressor
	// flow·(T − T_supply) becomes nearly a linear combination of the envelope regressor
	// (T_out − T) and the constant. Least squares on collinear regressors is not wrong so
	// much as under-determined: it splits the coefficient arbitrarily between them and still
	// fits the residuals. A real five-hour run ended up with negative envelope coupling,
	// POSITIVE cooling and intercepts near 100 °C/h — a model of a room that heats up when
	// you cool it.
	//
	// Two signs are known from first principles rather than from data: heat flows from hot
	// to cold, and cooling removes heat. A fit violating either has not discovered anything,
	// it has landed in the null space, so these bounds gate what may be TRUSTED. They are
	// deliberately not enforced by clamping the coefficients: projecting the estimate inside
	// the RLS recursion fights the covariance update, and when tried it drove the CO2 fit to
	// 1e20. The bounds detect the failure; the excitation gate below prevents it.
	envMinPerHour  = 0.02 // τ ≤ ~50 h
	envMaxPerHour  = 15.0 // τ ≥ ~4 min
	coolMaxPerHour = -0.005
	coolMinPerHour = -50.0
	achMinPerHour  = 0.06
	achMaxPerHour  = 25.0
	// --- excitation gating ---
	//
	// This is the fix for the collinearity above, and it works by refusing the useless data
	// rather than by patching the result.
	//
	// A sample only helps identify a room if the DRIVERS actually moved since the last one.
	// Under steady closed-loop control they do not: outdoor temperature is flat, the
	// controller parks the damper, occupancy is unchanged, and every fresh sample repeats
	// the previous one. Folding those in cannot improve the fit, but with a forgetting
	// factor it actively erodes it — the estimator discounts the informative history that
	// identified the room in favour of samples that say nothing.
	//
	// Crucially the test is on the INPUTS, not on the measured state. An earlier version
	// measured movement in the full regressor vector, which includes the noisy sensor
	// reading — so sensor noise registered as "excitation" and the gate waved through
	// exactly the samples it existed to reject. Persistent excitation is a property of the
	// input signal; that is what these thresholds measure. Each is the change that counts
	// as one unit of genuine movement for that driver.
	// These are deliberately SMALL. Their job is only to reject samples that repeat the
	// previous one exactly — a genuinely zero-information update. Set them coarse (0.5 °C /
	// 5% flow / 1 person) and a steady building identifies nothing at all: a live run
	// accepted a single sample per room in ten minutes. Stability under collinearity is the
	// ridge term's job, below, not this gate's.
	excOutdoorC  = 0.05 // °C
	excFlowRatio = 0.002
	excOccupancy = 1.0 // one person
	// A sample is also informative when the ROOM ITSELF is moving, even with every input
	// held constant: a room relaxing toward setpoint is executing a free response, and that
	// trajectory identifies its time constant. Gating on inputs alone misses this entirely
	// — a live run accepted 2 samples per room in ten minutes because the building was
	// simply sitting still while its zones cooled.
	//
	// The thresholds sit above the derivative noise floor that differencing produces at this
	// interval (±0.05 °C over 5 min is ~0.85 °C/h; ±8 ppm is ~135 ppm/h), so genuine
	// transients are accepted and sensor jitter is not.
	excTempRatePerHour = 1.0
	excCo2RatePerHour  = 150.0

	// --- ridge / leaky RLS: the actual cure for null-space drift ---
	//
	// Under collinear regressors the data pins down only SOME directions in parameter
	// space; along the rest the estimate is free, and with a forgetting factor it wanders
	// there indefinitely. That wandering is what turned a correct live model into one with
	// negative envelope coupling — the fit stayed excellent the whole way, because moving
	// along the null space costs no residual.
	//
	// So each update also leaks the estimate gently toward a physically sensible prior.
	// This is ridge regression in recursive form, and it is exactly a Bayesian prior: in
	// directions the data constrains, the data dominates and each room keeps its own
	// individual constants; in directions the data says nothing about, the estimate settles
	// on the prior instead of drifting. Rooms stay distinguishable where the evidence
	// distinguishes them, and stop inventing differences where it does not.
	dynamicsLeak = 0.002
)

// Physically sensible priors, in the same coefficient order as each model. A mid-range
// office room: ~2 h thermal time constant, real cooling authority, ~100 W of occupant heat,
// and ~2 air changes per hour.
var (
	thermalPrior = []float64{0.5, -1.0, 0.05, 0.2}
	co2Prior     = []float64{25.0, 2.0, 0.0}
)

// Driver scales, parallel to the driver vectors assembled in observeRoom.
var (
	thermalExcScales = []float64{excOutdoorC, excFlowRatio, excOccupancy}
	co2ExcScales     = []float64{excOccupancy}
)

// DynamicsMatureAfter exposes how many observations a room's fit needs before it is
// trusted, so the API and the exported bundle can report the same bar the engine applies.
func DynamicsMatureAfter() int { return dynamicsMature }

// --- recursive least squares -------------------------------------------------

// rlsState is a small recursive-least-squares estimator with exponential forgetting. It
// is the standard online system-identification recursion: exact least squares over all
// observations seen, updated in O(n²) per sample with no stored history. Exported fields
// so the whole learned state round-trips through JSON for persistence.
type rlsState struct {
	Theta  []float64 `json:"theta"` // fitted coefficients
	P      []float64 `json:"p"`     // n×n inverse-correlation matrix, row-major
	N      int       `json:"n"`     // observations folded in
	ResEma float64   `json:"res"`   // EMA of |residual|, the fit-quality readout
	// Skipped counts updates the excitation gate rejected, so the model can report how much
	// of the stream carried no identifying information.
	Skipped int `json:"skipped,omitempty"`
}

// excitedBy reports whether any driver has moved by at least one characteristic unit since
// the reference vector, and returns the drivers to remember when it has. cur and ref are
// parallel; scales gives each driver's unit of genuine movement.
func excitedBy(cur, ref, scales []float64) bool {
	if len(ref) != len(cur) {
		return true // nothing to compare against yet
	}
	for i := range cur {
		if math.Abs(cur[i]-ref[i]) >= scales[i] {
			return true
		}
	}
	return false
}

func newRLS(n int) *rlsState {
	s := &rlsState{Theta: make([]float64, n), P: make([]float64, n*n)}
	for i := 0; i < n; i++ {
		s.P[i*n+i] = dynamicsP0
	}
	return s
}

func (s *rlsState) dim() int { return len(s.Theta) }

// valid reports whether the estimator survived deserialization intact — a truncated or
// hand-edited state file must not be able to panic the engine.
func (s *rlsState) valid(n int) bool {
	return s != nil && len(s.Theta) == n && len(s.P) == n*n
}

func (s *rlsState) predict(x []float64) float64 {
	y := 0.0
	for i, xi := range x {
		y += s.Theta[i] * xi
	}
	return y
}

// update folds one (x, y) observation into the fit.
func (s *rlsState) update(x []float64, y float64, prior []float64) {
	n := s.dim()
	if len(x) != n || math.IsNaN(y) || math.IsInf(y, 0) {
		return
	}
	for _, xi := range x {
		if math.IsNaN(xi) || math.IsInf(xi, 0) {
			return
		}
	}

	// Px = P·x
	Px := make([]float64, n)
	for i := 0; i < n; i++ {
		sum := 0.0
		row := s.P[i*n : i*n+n]
		for j := 0; j < n; j++ {
			sum += row[j] * x[j]
		}
		Px[i] = sum
	}

	// info = xᵀ·P·x — how much this sample actually tells us. A near-zero value means the
	// regressors have not moved into any direction the estimator is still uncertain about.
	info := 0.0
	for i := 0; i < n; i++ {
		info += x[i] * Px[i]
	}
	// Forget only on informative samples (see dynamicsMinInfo): λ = 1 turns the update
	// into plain recursive least squares, which cannot inflate the covariance.
	lambda := dynamicsLambda
	if info < dynamicsMinInfo {
		lambda = 1.0
	}

	den := lambda + info
	if den < 1e-12 || math.IsNaN(den) || math.IsInf(den, 0) {
		return
	}

	err := y - s.predict(x)

	// θ ← θ + (P·x / denominator)·err
	for i := 0; i < n; i++ {
		s.Theta[i] += (Px[i] / den) * err
	}
	// P ← (P − P·x·xᵀ·P / denominator) / λ
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			s.P[i*n+j] = (s.P[i*n+j] - Px[i]*Px[j]/den) / lambda
		}
	}

	// Backstop: bound the covariance magnitude, preserving its shape.
	trace := 0.0
	for i := 0; i < n; i++ {
		trace += s.P[i*n+i]
	}
	if trace > dynamicsMaxTraceP && trace > 0 {
		scale := dynamicsMaxTraceP / trace
		for i := range s.P {
			s.P[i] *= scale
		}
	}

	// Guard against a divergent update poisoning the estimator permanently.
	for i := 0; i < n; i++ {
		if math.IsNaN(s.Theta[i]) || math.IsInf(s.Theta[i], 0) {
			*s = *newRLS(n)
			return
		}
	}

	s.N++
	a := 1.0 / float64(s.N)
	if a < dynamicsResAlphaFloor {
		a = dynamicsResAlphaFloor
	}
	s.ResEma += a * (math.Abs(err) - s.ResEma)

	// Ridge leak toward the prior (see dynamicsLeak), weighted by how UNCERTAIN each
	// coefficient still is. P[i][i] is the estimator's own variance for coefficient i, so a
	// direction the data has pinned down leaks essentially not at all and keeps the room's
	// individually measured value, while a direction the data never constrained — the null
	// space collinearity leaves behind — relaxes to the prior instead of drifting.
	//
	// An unweighted leak was tried first and was wrong: it dragged well-determined values
	// toward the prior too, pulling a genuinely under-ventilated room's measured 0.5 ACH up
	// to 0.89 and hiding the very fault the model exists to find.
	if len(prior) == n {
		for i := 0; i < n; i++ {
			w := s.P[i*n+i] / dynamicsP0
			if w > 1 {
				w = 1
			} else if w < 0 {
				w = 0
			}
			s.Theta[i] += dynamicsLeak * w * (prior[i] - s.Theta[i])
		}
	}
}

// --- per-room state ----------------------------------------------------------

// zoneDynamics holds one room's two fits plus the previous sample each finite difference
// is taken against.
type zoneDynamics struct {
	Thermal *rlsState `json:"thermal"`
	Co2     *rlsState `json:"co2"`

	LastTemp float64 `json:"lastTemp"`
	LastCo2  float64 `json:"lastCo2"`
	LastAt   float64 `json:"lastAt"`  // simulation seconds
	HasTemp  bool    `json:"hasTemp"` // a previous temperature sample exists
	HasCo2   bool    `json:"hasCo2"`

	// Drivers at the last ACCEPTED update of each fit, against which the next sample's
	// excitation is judged. They only advance on acceptance, so a slow but genuine drift
	// still accumulates until it clears the bar.
	LastThermalDrv []float64 `json:"lastThDrv,omitempty"`
	LastCo2Drv     []float64 `json:"lastCoDrv,omitempty"`
}

func newZoneDynamics() *zoneDynamics {
	return &zoneDynamics{Thermal: newRLS(4), Co2: newRLS(3)}
}

// RoomCondition is one room's instantaneous drivers, gathered by the engine under e.mu and
// handed to the independently-locked dynamics model — so the two locks are never held
// together (same discipline as ZoneReading / Baselines).
type RoomCondition struct {
	Zone      string
	Label     string
	Temp      float64
	Setpoint  float64
	OutdoorC  float64
	FlowRatio float64 // VAV flow as a fraction of its nominal flow (0..1+)
	Occupancy int
	Co2       float64
	Co2Live   bool // a fresh NDIR sensor is measuring this zone
}

// Dynamics is the concurrency-safe collection of per-room learned models. Like Baselines
// it owns its lock and never reaches back into the engine, preserving the one-way lock
// order e.mu → dynamics.mu.
type Dynamics struct {
	mu    sync.Mutex
	rooms map[string]*zoneDynamics
	// lastSampleAt is the simulation timestamp of the last accepted observation batch,
	// so the sampling cadence is enforced centrally rather than per room.
	lastSampleAt float64
	seeded       bool
}

func NewDynamics() *Dynamics {
	return &Dynamics{rooms: make(map[string]*zoneDynamics)}
}

// Observe folds one batch of room conditions into the learned models, if enough
// simulation time has passed since the last batch. simSeconds is the engine's accumulated
// simulation clock. Concurrency-safe.
func (d *Dynamics) Observe(conds []RoomCondition, simSeconds float64) {
	if math.IsNaN(simSeconds) || math.IsInf(simSeconds, 0) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.seeded && simSeconds-d.lastSampleAt < dynamicsSampleSimSecs {
		return
	}
	d.seeded = true
	d.lastSampleAt = simSeconds

	for _, c := range conds {
		if c.Zone == "" {
			continue
		}
		zd := d.rooms[c.Zone]
		if zd == nil {
			zd = newZoneDynamics()
			d.rooms[c.Zone] = zd
		}
		d.observeRoom(zd, c, simSeconds)
	}
}

// observeRoom takes the finite differences for one room and folds them in. Caller holds
// d.mu. Rates are expressed per HOUR: it keeps the regression well-conditioned and makes
// every learned coefficient directly readable (θ0 is 1/τ in hours; φ1 IS the air-change
// rate in air changes per hour).
func (d *Dynamics) observeRoom(zd *zoneDynamics, c RoomCondition, now float64) {
	dtSec := now - zd.LastAt
	fresh := zd.HasTemp && dtSec > 0 && dtSec <= dynamicsMaxGapSim
	dtHours := dtSec / 3600.0

	if math.IsNaN(c.Temp) || math.IsInf(c.Temp, 0) {
		return
	}

	if fresh {
		dTdt := (c.Temp - zd.LastTemp) / dtHours // °C per hour
		// Regressors, in the same order as the documented balance:
		//   (T_out − T_in), flow·(T_in − T_supply), occupancy, 1
		//
		// Evaluated at the MIDPOINT of the interval, which is not a cosmetic choice — it
		// removes an errors-in-variables bias that is otherwise fatal. The target
		// (T_t − T_{t−1})/Δt carries the sensor noise +ε_t/Δt, while a regressor built from
		// T_t carries −ε_t. Those correlate, injecting a spurious −σ²/Δt into the
		// covariance and dragging the coefficient negative — which is exactly how the CO2
		// fit below reached a NEGATIVE air-change rate under noisy input. Using the
		// midpoint (T_t + T_{t−1})/2 makes the leading error term E[ε_t² − ε_{t−1}²]/2Δt,
		// which is zero for white noise, so the bias cancels instead of accumulating.
		tMid := 0.5 * (c.Temp + zd.LastTemp)
		// Excitation gate on the DRIVERS (see the excitation constants). A sample where
		// nothing the room responds to has moved cannot identify anything, and folding it
		// in under a forgetting factor actively erodes what was already learned.
		drv := []float64{c.OutdoorC, c.FlowRatio, float64(c.Occupancy)}
		if excitedBy(drv, zd.LastThermalDrv, thermalExcScales) || math.Abs(dTdt) >= excTempRatePerHour {
			zd.LastThermalDrv = append(zd.LastThermalDrv[:0], drv...)
			x := []float64{
				c.OutdoorC - tMid,
				c.FlowRatio * (tMid - supplyAirC),
				float64(c.Occupancy),
				1.0,
			}
			zd.Thermal.update(x, dTdt, thermalPrior)
		} else {
			zd.Thermal.Skipped++
		}
	}

	if c.Co2Live && c.Co2 > 0 {
		if zd.HasCo2 && fresh {
			dCdt := (c.Co2 - zd.LastCo2) / dtHours // ppm per hour
			// Midpoint regressor, for the reason documented above: pairing the derivative
			// with the END-of-interval concentration correlates the regressor's noise with
			// the target's and biases the identified air-change rate negative.
			cMid := 0.5 * (c.Co2 + zd.LastCo2)
			// The only driver of a room's CO2 balance we observe is its occupancy. If that
			// has not changed, the concentration is just relaxing toward an equilibrium the
			// fit already knows, and the sample carries no new information about the
			// ventilation rate — so it is skipped rather than allowed to erode the fit.
			drv := []float64{float64(c.Occupancy)}
			if excitedBy(drv, zd.LastCo2Drv, co2ExcScales) || math.Abs(dCdt) >= excCo2RatePerHour {
				zd.LastCo2Drv = append(zd.LastCo2Drv[:0], drv...)
				x := []float64{
					float64(c.Occupancy),
					outdoorCo2Ppm - cMid,
					1.0,
				}
				zd.Co2.update(x, dCdt, co2Prior)
			} else {
				zd.Co2.Skipped++
			}
		}
		zd.LastCo2 = c.Co2
		zd.HasCo2 = true
	} else {
		// The sensor went away; don't difference across the outage when it returns.
		zd.HasCo2 = false
	}

	zd.LastTemp = c.Temp
	zd.HasTemp = true
	zd.LastAt = now
}

// --- interpreting the fits ---------------------------------------------------

// thermalUsable reports whether a thermal fit is mature AND physically sane. An
// identification run can converge on nonsense if a room never moved, and a nonsensical
// model must never be allowed to raise an alarm or make a prediction.
func (zd *zoneDynamics) thermalUsable() bool {
	if zd == nil || !zd.Thermal.valid(4) || zd.Thermal.N < dynamicsMature {
		return false
	}
	env, cool := zd.Thermal.Theta[0], zd.Thermal.Theta[1]
	// Two signs are known from first principles rather than from data: heat flows from hot
	// to cold, and cooling removes heat. A fit violating either has not discovered
	// something surprising, it has landed in the null space of an under-determined problem
	// — so it is reported as NOT identified rather than used to predict.
	return env > envMinPerHour && env < envMaxPerHour && cool < coolMaxPerHour
}

func (zd *zoneDynamics) co2Usable() bool {
	if zd == nil || !zd.Co2.valid(3) || zd.Co2.N < dynamicsMature {
		return false
	}
	// A physically plausible air-change rate: a sealed room is ~0.1 ACH, an aggressive
	// purge is ~20 ACH. Outside that band the fit has not identified ventilation.
	ach := zd.Co2.Theta[1]
	return ach > achMinPerHour && ach < achMaxPerHour
}

// RoomModel is the JSON-facing view of one room's learned physical identity — what the
// dashboard renders and the exported bundle carries.
type RoomModel struct {
	Zone  string `json:"zone"`
	Label string `json:"label"`

	ThermalSamples   int     `json:"thermalSamples"`
	ThermalReady     bool    `json:"thermalReady"`
	TimeConstantMin  float64 `json:"timeConstantMin"`  // 1/θ0, minutes
	CoolingAuthority float64 `json:"coolingAuthority"` // −θ1, per hour per unit flow·ΔT
	PerOccupantC     float64 `json:"perOccupantC"`     // θ2, °C/h per person
	ThermalResidual  float64 `json:"thermalResidual"`  // mean |error| in °C/h

	Co2Samples     int     `json:"co2Samples"`
	Co2Ready       bool    `json:"co2Ready"`
	AchPerHour     float64 `json:"achPerHour"`     // φ1, air changes per hour
	PerOccupantPpm float64 `json:"perOccupantPpm"` // φ0, ppm/h per person
	Co2Residual    float64 `json:"co2Residual"`

	// Coefficients verbatim, so a downloaded copy of the model can reproduce every
	// prediction the server makes without re-deriving anything.
	ThermalTheta []float64 `json:"thermalTheta"`
	Co2Theta     []float64 `json:"co2Theta"`
}

// RoomModels renders every room's learned identity, most-identified first. Concurrency-safe.
func (d *Dynamics) RoomModels(labels map[string]string) []RoomModel {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]RoomModel, 0, len(d.rooms))
	for zone, zd := range d.rooms {
		if !zd.Thermal.valid(4) || !zd.Co2.valid(3) {
			continue
		}
		m := RoomModel{
			Zone:            zone,
			Label:           labels[zone],
			ThermalSamples:  zd.Thermal.N,
			ThermalReady:    zd.thermalUsable(),
			ThermalResidual: zd.Thermal.ResEma,
			Co2Samples:      zd.Co2.N,
			Co2Ready:        zd.co2Usable(),
			Co2Residual:     zd.Co2.ResEma,
			ThermalTheta:    append([]float64(nil), zd.Thermal.Theta...),
			Co2Theta:        append([]float64(nil), zd.Co2.Theta...),
		}
		if m.Label == "" {
			m.Label = zone
		}
		if zd.thermalUsable() {
			m.TimeConstantMin = 60.0 / zd.Thermal.Theta[0]
			m.CoolingAuthority = -zd.Thermal.Theta[1]
			m.PerOccupantC = zd.Thermal.Theta[2]
		}
		if zd.co2Usable() {
			m.AchPerHour = zd.Co2.Theta[1]
			m.PerOccupantPpm = zd.Co2.Theta[0]
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ThermalSamples > out[j].ThermalSamples
	})
	return out
}

// Coverage reports how many rooms have an identified (trusted) model vs. how many are
// still being identified — the honest maturity readout, matching Baselines.Coverage.
func (d *Dynamics) Coverage() (identified, learning int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, zd := range d.rooms {
		if zd.thermalUsable() || zd.co2Usable() {
			identified++
		} else {
			learning++
		}
	}
	return
}

// --- forward prediction ------------------------------------------------------

// ThermalOutlook is the learned forward prediction for one room: where it settles under
// the drivers it has right now, where it will be at the horizon, and how long until it
// crosses a limit. All of it is closed-form arithmetic on the learned coefficients.
type ThermalOutlook struct {
	Ok          bool
	Equilibrium float64 // °C the room trends to if nothing changes (clamped to physical range)
	Predicted   float64 // °C at the requested horizon (clamped)
	TauMin      float64 // effective time constant under the current flow, minutes
	SecsToLimit float64 // seconds until it crosses limit; <0 = it never does
	Samples     int
	// Extrapolated marks an equilibrium that sits outside anything this room has been
	// observed doing. A linear fit has no idea it is being asked to extrapolate: hold a
	// busy room at 15% airflow forever and the arithmetic will happily promise 50°C. The
	// crossing TIME stays trustworthy in that situation (it depends on the early part of
	// the curve, which is inside the identified regime) but the asymptote does not, so the
	// caller must present it as a direction of travel rather than a number.
	Extrapolated bool
}

const (
	// The physical range the engine's own integration clamps to (engine.go tick). A
	// prediction has no business claiming anything outside the band the twin can represent.
	physMinC = 5.0
	physMaxC = 50.0
	// credibleMaxC is where a predicted settling point stops being a forecast and starts
	// being an extrapolation artefact worth labelling as one.
	credibleMaxC = 45.0
)

func clampC(v float64) float64 { return math.Max(physMinC, math.Min(physMaxC, v)) }

// ThermalOutlookFor integrates the learned first-order response forward in closed form.
//
// With the drivers held constant the balance is dT/dt = k·(T_eq − T), where the effective
// rate k = θ0 − θ1·flow (positive: envelope coupling plus the cooling the VAV is
// delivering), so T(t) = T_eq + (T0 − T_eq)·e^(−k·t) exactly.
func (d *Dynamics) ThermalOutlookFor(c RoomCondition, limit, horizonSec float64) ThermalOutlook {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.thermalOutlookLocked(c, limit, horizonSec)
}

// thermalOutlookLocked is the same computation with d.mu already held, so a pass over
// every room takes the lock once rather than once per room. Caller must hold d.mu.
func (d *Dynamics) thermalOutlookLocked(c RoomCondition, limit, horizonSec float64) ThermalOutlook {
	zd := d.rooms[c.Zone]
	if !zd.thermalUsable() {
		return ThermalOutlook{}
	}
	th := zd.Thermal.Theta
	env, cool, occ, base := th[0], th[1], th[2], th[3]

	// k in per-hour units; positive because env > 0 and cool < 0.
	k := env - cool*c.FlowRatio
	if k <= 1e-6 {
		return ThermalOutlook{}
	}
	// Solving 0 = env·(T_out − T) + cool·flow·(T − T_supply) + occ·people + base for T.
	eq := (env*c.OutdoorC - cool*c.FlowRatio*supplyAirC + occ*float64(c.Occupancy) + base) / k
	if math.IsNaN(eq) || math.IsInf(eq, 0) {
		return ThermalOutlook{}
	}

	hHours := horizonSec / 3600.0
	predicted := eq + (c.Temp-eq)*math.Exp(-k*hHours)

	out := ThermalOutlook{
		Ok:           true,
		Equilibrium:  clampC(eq),
		Predicted:    clampC(predicted),
		TauMin:       60.0 / k,
		SecsToLimit:  -1,
		Samples:      zd.Thermal.N,
		Extrapolated: eq > credibleMaxC || eq < physMinC,
	}

	// Time to cross the limit, if it ever does. Only meaningful when the room is heading
	// toward the limit from the safe side and its equilibrium actually lies beyond it.
	if eq > limit && c.Temp < limit {
		ratio := (limit - eq) / (c.Temp - eq) // both negative → positive, < 1
		if ratio > 0 && ratio < 1 {
			out.SecsToLimit = -math.Log(ratio) / k * 3600.0
		}
	}
	return out
}

// Co2Outlook is the learned ventilation prediction for one room.
type Co2Outlook struct {
	Ok          bool
	Equilibrium float64 // ppm the room settles at under its current occupancy (clamped)
	Predicted   float64 // ppm at the requested horizon (clamped)
	AchPerHour  float64 // learned air-change rate
	SecsToLimit float64 // seconds until it crosses limit; <0 = it never does
	Samples     int
	// Extrapolated: same caveat as the thermal outlook. A very low identified ACH divided
	// into a large occupancy load produces an arithmetically valid but operationally
	// meaningless asymptote, so it is labelled rather than quoted as fact.
	Extrapolated bool
}

const (
	// co2CredibleMax is roughly the worst a real occupied room reaches; beyond this the
	// balance is extrapolating rather than forecasting.
	co2CredibleMax = 5000.0
	co2PhysMax     = 40000.0
)

func clampPpm(v float64) float64 {
	return math.Max(outdoorCo2Ppm, math.Min(co2PhysMax, v))
}

// Co2OutlookFor is the same closed-form integration for the mass balance:
// dC/dt = φ1·(C_out − C) + (φ0·people + φ2), which settles at
// C_eq = C_out + (φ0·people + φ2)/φ1 and approaches it at rate φ1.
func (d *Dynamics) Co2OutlookFor(c RoomCondition, limit, horizonSec float64) Co2Outlook {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.co2OutlookLocked(c, limit, horizonSec)
}

// co2OutlookLocked is the lock-held variant, for whole-building passes. Caller holds d.mu.
func (d *Dynamics) co2OutlookLocked(c RoomCondition, limit, horizonSec float64) Co2Outlook {
	zd := d.rooms[c.Zone]
	if !zd.co2Usable() {
		return Co2Outlook{}
	}
	ph := zd.Co2.Theta
	gen, ach, base := ph[0], ph[1], ph[2]

	eq := outdoorCo2Ppm + (gen*float64(c.Occupancy)+base)/ach
	if math.IsNaN(eq) || math.IsInf(eq, 0) {
		return Co2Outlook{}
	}
	hHours := horizonSec / 3600.0
	predicted := eq + (c.Co2-eq)*math.Exp(-ach*hHours)

	out := Co2Outlook{
		Ok: true, Equilibrium: clampPpm(eq), Predicted: clampPpm(predicted),
		AchPerHour: ach, SecsToLimit: -1, Samples: zd.Co2.N,
		Extrapolated: eq > co2CredibleMax,
	}
	if eq > limit && c.Co2 < limit {
		ratio := (limit - eq) / (c.Co2 - eq)
		if ratio > 0 && ratio < 1 {
			out.SecsToLimit = -math.Log(ratio) / ach * 3600.0
		}
	}
	return out
}

// --- learned setback depth ---------------------------------------------------

const (
	// setbackMinC / setbackMaxC bound what the learned answer may recommend. The floor
	// keeps a setback worth having; the ceiling stops an unrealistically fast-recovering
	// fit from parking a room somewhere a returning occupant would notice on the way back.
	setbackMinC = 1.5
	setbackMaxC = 6.0
	// setbackSafetyC is held back from the theoretical maximum. The recovery calculation
	// assumes the room gets full flow the moment it is called for, which a shared AHU
	// under morning load does not always deliver.
	setbackSafetyC = 0.5
)

// SetbackCeiling answers, for one room: how far above its occupied setpoint can this room
// be parked while vacant, and still be back at setpoint within recoverSec once someone
// returns?
//
// This replaces a fixed "+4 °C for every zone", which is the same category of mistake the
// baselines replaced — a number that is simultaneously too timid for a light, responsive
// room and too aggressive for a heavy one that cannot recover before the floor fills up.
// The room's own identified response answers it directly. Recovery at full cooling obeys
// T(t) = T_eq + (T_sb − T_eq)·e^(−k·t) with k = θ₀ − θ₁ (full flow), so the deepest
// recoverable start point is T_sb = T_eq + (setpoint − T_eq)·e^(k·t_recover).
//
// ok is false when the room has no trusted model or cannot reach setpoint at all, and the
// caller keeps its conventional fixed setback — the same discipline as everywhere else
// here: act on the learned value only when there genuinely is one.
func (d *Dynamics) SetbackCeiling(zone string, setpoint, outdoorC float64, recoverSec float64) (deltaC float64, ok bool) {
	if recoverSec <= 0 || setpoint <= 0 {
		return 0, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	zd := d.rooms[zone]
	if !zd.thermalUsable() {
		return 0, false
	}
	th := zd.Thermal.Theta
	env, cool, occ, base := th[0], th[1], th[2], th[3]

	// Recovery is evaluated at FULL flow and with the room empty — the conditions that
	// actually apply while it is being brought back before occupants arrive.
	k := env - cool*1.0
	if k <= 1e-6 {
		return 0, false
	}
	eqFull := (env*outdoorC - cool*1.0*supplyAirC + occ*0 + base) / k
	// A room that cannot get below its own setpoint even at full cooling has no recovery
	// margin to spend; setting it back at all risks never catching up.
	if eqFull >= setpoint-0.1 {
		return 0, false
	}

	hours := recoverSec / 3600.0
	tSb := eqFull + (setpoint-eqFull)*math.Exp(k*hours)
	delta := tSb - setpoint - setbackSafetyC
	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		return 0, false
	}
	if delta < setbackMinC {
		// The room is too sluggish to recover from a worthwhile setback in the budget.
		// Reporting the floor is wrong here — say so and let the caller decide.
		return setbackMinC, true
	}
	if delta > setbackMaxC {
		delta = setbackMaxC
	}
	return delta, true
}

// --- persistence -------------------------------------------------------------

// MarshalState / LoadState mirror the baseline model's byte-level persistence surface, so
// the identified room models survive a restart and package main never needs to name the
// unexported per-room type. Rooms whose fits are still trivially young are dropped to keep
// the snapshot small.
func (d *Dynamics) MarshalState() ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	const keepAbove = 5
	out := make(map[string]*zoneDynamics, len(d.rooms))
	for zone, zd := range d.rooms {
		if zd.Thermal.N > keepAbove || zd.Co2.N > keepAbove {
			out[zone] = zd
		}
	}
	return json.Marshal(out)
}

func (d *Dynamics) LoadState(data []byte) error {
	var snap map[string]*zoneDynamics
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rooms = make(map[string]*zoneDynamics, len(snap))
	for zone, zd := range snap {
		if zd == nil || !zd.Thermal.valid(4) || !zd.Co2.valid(3) {
			continue // discard anything that would not be safe to run
		}
		// A restored room has no trustworthy predecessor sample in THIS process, so the
		// next observation must not be differenced against a stale one.
		zd.HasTemp, zd.HasCo2, zd.LastAt = false, false, 0
		d.rooms[zone] = zd
	}
	return nil
}
