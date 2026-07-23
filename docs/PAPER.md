# Identifying rooms instead of scheduling them: a per-room learning digital twin for Vietnamese office buildings

**Abstract.** Vietnamese office buildings consume 105.9–116.4 kWh/m²·yr and the building
sector accounts for 37–40% of national final energy use, against a national commitment to
cut greenhouse gas emissions 43.5% against business-as-usual by 2030. Conventional Building
Management Systems (BMS) address this poorly for two measurable reasons. First, they do not
see the largest load: in a metered 45-storey Hanoi tower running a full BMS over chillers,
towers, pumps, fans and lifts, *plug loads* were the single largest end use at 26.4% of
energy — a category with no BMS meter and no BMS actuator. Second, their control strategies
are commissioned once and do not transfer: field tests on a tropical office measured the
same condenser-water strategy gaining 3% in the dry season and **losing 7%** in the wet,
while occupant thermal comfort was compromised by the very strategies that saved energy. We
present ECON, a digital twin that replaces fixed rules with per-room online system
identification. Each room's thermal and CO₂ balances are fitted by recursive least squares
with a forgetting factor, yielding physically interpretable coefficients — thermal time
constant, cooling authority, measured air-change rate — from which vacancy setback depth is
*solved* rather than assumed, and refused where a room cannot recover. The system enforces a
measured-versus-modelled discipline in code: a modelled value may never travel on a channel
implying measurement. We report what is verified against ground truth, what runs on physical
hardware, and what is not yet demonstrated.

**Keywords:** building energy management, system identification, recursive least squares,
digital twin, demand-controlled ventilation, plug load, Vietnam

---

## 1. Introduction

Vietnam committed at COP26 to net-zero emissions by 2050. Decision 896/QĐ-TTg, the National
Strategy on Climate Change, translates that into a binding intermediate target: a 43.5%
reduction in national greenhouse gas emissions against business-as-usual by 2030, with the
energy sector cutting 32.6% to no more than 457 Mt CO₂e. The same decision makes reduction
*mandatory* for any facility emitting ≥ 2,000 t CO₂e per year.

Buildings are 37–40% of Vietnam's total final energy consumption [1], and the grid remains
over 60% coal and gas, so a kilowatt-hour saved in a Vietnamese office carries more carbon
than the same kilowatt-hour saved in a temperate market. The national grid emission factor
is 0.6766 tCO₂/MWh [1].

This paper argues that the conventional BMS response to this problem is structurally
limited, that the limitation is measurable rather than rhetorical, and that per-room online
identification addresses it. Section 2 establishes the gap from three field studies. Section
3 describes the system. Section 4 gives the identification method. Sections 5–7 report
implementation, results and limitations.

## 2. The measurable gap in conventional BMS

### 2.1 The largest end use is invisible to the BMS

Luong et al. [1] metered a 45-storey, 117,000 m² Hanoi office tower from September 2024 to
May 2025. The building is not unmanaged: it runs water-cooled chillers, high-performance LED
lighting with daylight sensors, and a BMS controlling *"chillers and cooling towers of air
conditioning, lighting, pumps, ventilation fans, elevators."*

| End use | Share of energy |
|---|---|
| **Plug loads** | **26.4%** |
| Air-conditioning | 25.1% |
| Outdoor lighting | 16.1% |
| Ventilation | 15.3% |
| Indoor lighting | 9.1% |
| Elevators | 7.8% |
| Domestic water pumps | 0.2% |

The largest end use in a fully BMS-managed tower is the one category the BMS has neither a
meter nor an actuator on. A BMS manages *plant*; plug load sits downstream of the socket
outlet. The paper's own recommendation — *"automated plug load controls, awareness programs,
and shutdown protocols"* — describes a capability a plant-level BMS does not have.

> **A correction to the source.** Luong et al. report one set of end-use shares for energy
> (§3.2) and a different set for CO₂ (§3.3): indoor lighting 9.1% of energy but 16% of
> emissions, outdoor lighting 16.1% of energy but 0.4% of emissions. Applying a *single*
> grid emission factor, as the paper does, carbon share must equal energy share. The two
> cannot both be correct, and the pattern suggests indoor/outdoor labels were transposed in
> one figure. We use the energy shares throughout as the internally consistent set.

### 2.2 Strategies do not transfer across seasons

Meng et al. [3] field-tested three BMS control strategies on a mixed-use office in Darwin, a
hot-humid tropical climate comparable to southern Vietnam, reporting each by season:

| Strategy | Wet season | Dry season |
|---|---|---|
| Space temperature setpoint reset | +8% | +11% |
| Chilled water delivery temperature optimisation | +5% | +7% |
| **Condenser water delivery temperature optimisation** | **−7%** | +3% |

The same unchanged strategy lost 7% in one season and gained 3% in the other. A rule
commissioned once is not merely suboptimal for part of the year; it can be actively
negative, and nothing in the BMS reports it. The same authors note that a Canberra retrofit
saving 60% of annual energy *"has a drastically different climate to Darwin and no similar
current studies have been conducted in a hot humid tropical climate"* — strategies validated
in temperate markets arrive in Vietnam unvalidated.

### 2.3 Comfort fails silently

From the same study: *"the thermal comfort of the occupants was compromised during the wet
season when both these strategies were implemented."* A plant-optimising BMS measures the
plant. Without a per-room model of whether a given room can still recover to setpoint, it
cannot distinguish a saving from a comfort failure until an occupant complains. The
literature the same paper cites is blunter still: *"the BMS is not used to its full potential
in many buildings, performing little more than time clock functions."*

Hoang et al. [2] surveyed 57 commercial and government offices in Hanoi and Ho Chi Minh
City, finding mean energy use intensity of 105.9, 116.4 and 109.6 kWh/m²·yr for Hanoi, HCMC
and both, with monthly intensity tracking ambient air temperature — precisely the
sensitivity a fixed setpoint cannot express.

## 3. System

ECON is a Go physics engine, a React operator interface, and a fleet of ESP32/RP2040 edge
nodes on MQTT.

The engine integrates a two-resistance one-capacitance (2R1C) heat balance per zone over a
digitized building, ingests live telemetry where physical nodes exist, and actuates lighting
relays, socket circuits and split air conditioners via vendor infrared frames. A computer
vision branch (YOLOv8 + ByteTrack) publishes occupant head counts on the same MQTT contract
as a hardware node, so a camera is one more node to the twin.

**Building coefficients are data, not code.** Every lighting power density, U-value,
ventilation rate, setpoint and plant COP lives in a versioned programme library
(`server/data/programme-library.json`) with its source recorded. Physics stays in the
engine; the coefficients it is evaluated with are re-calibrated by editing data.

## 4. Method: per-room online identification

### 4.1 The two balances

For each zone, two first-order balances are fitted online:

```
thermal:  dT/dt = θ₀·(T_out − T_in) + θ₁·flow·(T_in − T_supply) + θ₂·occupancy + θ₃
co2:      dC/dt = φ₀·occupancy + φ₁·(C_out − C) + φ₂
```

The coefficients are physically interpretable: 1/θ₀ is the room's thermal time constant, θ₁
its cooling authority, θ₂ the per-occupant heat gain, and φ₁ the room's **measured air-change
rate**. These are not tuning parameters; they are properties of the room recovered from its
own behaviour.

### 4.2 Estimator

Coefficients are updated by recursive least squares with forgetting factor λ = 0.999, so the
fit tracks seasonal change rather than averaging it away — directly addressing the transfer
failure in §2.2. Several safeguards proved necessary in practice:

- **Midpoint regressors.** Using `T_mid = ½(T_k + T_{k−1})` rather than the endpoint cancels
  the errors-in-variables bias that shared measurement noise otherwise injects between
  regressor and target.
- **Excitation gating.** An observation is accepted only when a *driver* moved (outdoor
  temperature, flow, occupancy) or the state itself is in transient. Regressor-based gating
  admits sensor noise as though it were excitation.
- **Uncertainty-weighted ridge.** A leak toward a physical prior, weighted by each
  coefficient's own covariance `P[i][i]/P₀`, so well-determined coefficients are left alone
  while poorly-excited ones are prevented from wandering in the null space that closed-loop
  operation creates.
- **Sample spacing** of 300 s, chosen so the signal-to-noise ratio of a finite-difference
  derivative is adequate — not for responsiveness.

### 4.3 What the coefficients are used for

Vacancy setback depth is **solved**, not configured: `SetbackCeiling()` returns how far a
room may drift and still return to setpoint within the recovery budget, given that room's own
identified response. A light, responsive room earns a deeper setback; a heavy one gets a
shallower one; a room that cannot recover is **refused** a setback. This is the direct
answer to §2.3 — comfort cannot fail silently if the setback is bounded by the room's
measured recovery capability.

### 4.4 The measured-versus-modelled discipline

A modelled value must never travel on a channel implying it was measured. This is enforced
mechanically, not by convention:

- Firmware **omits** a field when its sensor is absent or fails, rather than sending a
  plausible default. A fabricated zero on a current clamp would tell the twin the compressor
  is off.
- `tempReal` gates whether a temperature may pin zone physics; `Co2Live` gates whether a
  reading may train the CO₂ balance, so the model is never fitted to the twin's own estimate.
- `acReal` reports whether a setpoint command actually reached an air conditioner, so a
  setback terminating in a log line is never credited with a saving.

## 5. Implementation

### 5.1 Building fixture

The bundled building is a 15-storey, 39,777 m², 735-zone office derived from a real floor
plan by DeepFloorplan segmentation. The segmenter locates room *boundaries* well and names
their *function* poorly: it labelled 555 closets averaging 4.2 m² as server rooms at 85 kW
each — 14,768 W/m², and 86% of the building's connected load.

A post-processing step (`tools/officeize_fixture.py`) keeps every polygon and re-derives
programme and physics from geometry against the library: internal gain from published power
densities, air capacitance from each room's actual volume, and envelope resistance from each
zone's own façade, roof and partition area. Calibration target is the measured cohort [2].

### 5.2 Edge hardware

Nodes are ESP32-WROOM-32 with SHT30 temperature/humidity (±0.2 °C), Ai-Thinker Rd-03 24 GHz
radar presence (which holds a *stationary* occupant), ASAIR ACD1200 NDIR CO₂, an SCT-013
split-core current transformer for plug metering, opto-isolated relays, and an infrared
emitter driving vendor AC protocols. Total ≈ 590,000 VND per standard node at Vietnamese
retail. Three further sensors are implemented in firmware to replace remaining assumptions
with measurements: a DS18B20 supply-air probe, a second current clamp on the air
conditioner's own supply, and a BH1750 illuminance sensor.

## 6. Results

We separate what is verified from what is not, because the distinction is the point.

### 6.1 Verified

**The estimator recovers known dynamics.** A test suite of 42 cases in the simulation package
passes. `TestDynamicsIdentifiesKnownRoom` recovers a synthetic room's thermal time constant
and cooling authority to **within 15%** of ground truth;
`TestDynamicsIdentifiesAirChangeRate` recovers air-change rate to the same tolerance;
`TestDynamicsPredictsActualCrossing` predicts a threshold-crossing time within 20%;
`TestSetbackRefusedWhenRoomCannotRecover` confirms the capability refusal;
`TestIdentificationSurvivesClosedLoopOperation` is a regression test for a live failure in
which closed-loop collinearity destroyed the fit.

**Fixture calibration.** Design-day analysis gives 3,266 occupants at 12.2 m²/person, 3,283
kW thermal load (lighting 409, occupants 327, solar 9, plug 384, fresh air 2,155) and a
1,654 kW electrical peak, equivalent to **≈125 kWh/m²·yr** at 3,000 equivalent full-load
hours against a cohort of 109.6 [2]. Correcting the mis-digitized fixture moved reported
grid power from **15.2 MW to 0.7 MW** for the same building.

**Envelope realism.** Deriving resistance from geometry places **93% of zones in the 1–40 h**
time-constant band a real office exhibits, with genuine spread (0.7–10.8 h), against a prior
fixture whose flat resistance gave every zone the same value and a simulated time constant
of 1,300 h.

**Fresh-air load dominates.** At design occupancy the outdoor-air load is 2,155 kW of a
3,283 kW total — 66%, and mostly latent. A twin calibrated on internal gains alone
under-reports a tropical office by roughly a third.

**Hardware.** Two physical edge nodes publish live telemetry into the twin, and vendor IR
frames reach a real air conditioner.

### 6.2 Not demonstrated

**Identification is converging but has not matured.** Maturity requires 36 samples at 300
simulated seconds; with simulation time at 1× real this is ~3 hours of continuous uptime.
Convergence against sample count on the live instance, across all 735 zones:

| Samples per room (median) | Implied τ median | In 1–40 h band | Negative per-occupant | Cooling sign correct |
|---|---|---|---|---|
| 1 | 375 h | 7% | 88% | 90% |
| 3 (max 20) | **1.1 h** | **70%** | **4%** | **96%** |

This is the expected behaviour of a recursive estimator — at one sample it is the prior plus
noise — and it is also evidence that the earlier pathology was in the *model*, not the
estimator. On the previous fixture, whose flat envelope resistance gave every zone a
simulated time constant near 1,300 h, 36% of rooms reported that occupants *cool* the room.
With envelope resistance derived from geometry, that falls to 4% and the cooling-authority
sign is correct in 96% of rooms. The identifier had been faithfully recovering the physics of
a building modelled as a thermos.

**No room has yet passed the maturity gate** (median 3 of 36 samples at time of writing), so
no identified coefficient is in use by the controller. We report convergence, not
convergence *achieved*.

**Projected rather than measured savings.** Applying reduction fractions to the measured
end-use shares of [1] gives ≈11.9% of building electricity: plug loads 6.6% (26.4% × 25%),
air-conditioning 3.0% (25.1% × 12%), indoor lighting 2.3% (9.1% × 25%). The end-use shares
are measured; **the reduction fractions are assumptions.** Only the air-conditioning figure
has independent support: [3] measured 8–11% from setpoint reset alone on central plant. At
the Hanoi tower's peak month (219.4 t CO₂) an 11.9% cut is ≈26 t CO₂ avoided in one month.

## 7. Limitations

- **No measurement and verification baseline.** Savings are a model counterfactual, never
  compared against a measured baseline period under IPMVP or equivalent. This is the single
  largest gap between the projection above and a defensible claim.
- **No demand-controlled ventilation loop.** CO₂ is measured and air-change rate identified,
  but nothing acts on either. Ventilation is 15.3% of the case-study building's energy and is
  deliberately excluded from the projection in §6.2 for this reason.
- **CO₂ identification is unreachable without hardware.** Only a live NDIR may train the CO₂
  balance, by design. In a simulated building no zone qualifies.
- **Occupancy is presence, not count, without a camera.** θ₂ and φ₀ are per-*occupant*
  coefficients; a binary radar signal identifies them per *occupied room*.
- **Solar gain is static.** A BH1750 illuminance sensor is ingested but does not yet drive
  solar gain.
- **Firmware is not deployment-ready.** No over-the-air update, MQTT is anonymous, per-board
  identity is compiled in, and there is no local fail-safe when the broker is unreachable.
- **The partition coupling coefficient is a calibration choice**, stated as one in the
  library, not a measured U-value.

## 8. Conclusion

The case for per-room identification does not rest on a claim that machine learning is
superior to rules. It rests on two measurements: that the largest end use in a
BMS-managed Vietnamese office tower is one the BMS cannot see, and that a control strategy
validated in one season of a tropical climate lost 7% in the other. Both are failures of
*fixity*, not of sophistication. A system that identifies each room continuously, bounds its
actions by that room's measured recovery capability, and refuses to credit itself for
commands that reached no machine, addresses the failure at its cause.

The identification mathematics is verified against ground truth, and the live estimator
converges toward physical coefficients as samples accumulate. What remains is a
sufficiently long live deployment to demonstrate it on a real building, and the measurement
and verification baseline that would turn a projection into a result.

## References

[1] Nguyen Duc Luong, Nguyen Thi Hue, Nguyen Huy Tien, Nguyen Van Duy, Hoang Xuan Hoa (2025).
*Assessing energy consumption and operational carbon emission: A case study of office
building in Hanoi.* Journal of Materials and Construction 15(02).
doi:10.54772/jomc.v15i02.1190

[2] Hoang Tuan Viet, Nguyen Duy Dong, Nguyen My Anh, Nguyen Duc Luong, Tran Ngoc Quang, Mac
Van Dat, Joseph J. Deringer (2021). *A study of energy consumption for office buildings in
Vietnam for sustainable energy and climate change mitigation.* Proceedings of the
International Conference on Evolving Cities, 63–70. doi:10.55066/proc-icec.2021.19

[3] Li Meng, Rita Yi Man Li, William Finocchiaro, Alemu Alemu (2023). *Integrating Building
Management Systems (BMS) and BIM to improve HVAC efficiency: a case study in Darwin.* EBIMCS
2023, ACM. doi:10.1145/3644479.3644494

[4] Socialist Republic of Vietnam (2022). *Decision 896/QĐ-TTg approving the National
Strategy on Climate Change to 2050.*

[5] QCVN 09:2017/BXD — National Technical Regulation on Energy Efficiency Buildings.
