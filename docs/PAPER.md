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

For each zone, two first-order balances are fitted online — one on the thermal energy
balance, one on the CO₂ mass balance. Both are linear in their parameters; §5.6 gives them
in regression form together with the estimator.

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

---

## 5. Mathematical foundations

Notation is uniform throughout: $T$ is zone air temperature (°C), $T_o$ outdoor air, $T_s$
supply air, $C$ zone CO₂ (ppm), $n$ occupant count, $u\in[0,1]$ the cooling flow fraction,
and $\Delta t$ the integration step (s).

### 5.1 Zone thermal network (2R1C)

Each zone is a two-resistance, one-capacitance network: an air node coupled to a wall node,
the wall node coupled to outdoors. Writing $C_a$ for air capacitance, $C_w$ for wall
capacitance and $R_i,R_o$ for the inside and outside resistances,

$$
C_a\frac{dT}{dt} \;=\; \frac{T_w - T}{R_i} \;+\; \dot{Q}_{\text{int}} \;-\; \dot{Q}_{\text{cool}}
$$

$$
C_w\frac{dT_w}{dt} \;=\; \frac{T_o - T_w}{R_o} \;-\; \frac{T_w - T}{R_i}
$$

integrated by explicit Euler. Internal gain is the sum of the terms the programme library
defines, with no double counting between them:

$$
\dot{Q}_{\text{int}} \;=\; \underbrace{A\,\rho_L}_{\text{lighting}} \;+\; \underbrace{P_{\text{fix}}}_{\text{fixed equip.}} \;+\; \underbrace{n\,q_{\text{occ}}}_{\text{people}} \;+\; \underbrace{\sigma_{\text{sol}}\,\rho_S A}_{\text{solar}} \;+\; \underbrace{P_{\text{plug}}}_{\text{plug load}}
$$

where $A$ is floor area, $\rho_L$ lighting power density (W/m²), $q_{\text{occ}}=100$ W the
sensible gain per occupant, and $\sigma_{\text{sol}}$ the zone's façade exposure factor.

**Capacitance** is derived from the room's own volume rather than assigned per type:

$$
C_a \;=\; \kappa\,\rho c_p\,A\,h,\qquad \rho c_p = 1206~\mathrm{J\,m^{-3}K^{-1}},\quad \kappa = 5
$$

The multiplier $\kappa$ lumps furniture, partitions and slab surface that couple to the air;
room air alone gives a time constant of minutes, where real rooms respond over hours.

**Resistance** is derived from the zone's own exposed area:

$$
(UA)_{\text{zone}} \;=\; U_w A_w \;+\; U_r A_r \;+\; U_p A_p, \qquad R \;=\; \frac{1}{(UA)_{\text{zone}}}
$$

$$
R_i = f_R\,R,\qquad R_o = (1-f_R)\,R,\qquad f_R = 0.4
$$

with $A_w$ the façade area (from polygon edges lying on the building outline), $A_r$ roof
area on the top floor, and $A_p$ partition area. $U_p$ is an *effective* coupling well below
the assembly U-value, because the 2R1C has a single outdoor node: conductance to a neighbour
at nearly the same temperature must enter as a smaller coupling to outdoors, or a core zone
is modelled as though the room next door were the weather.

The resulting open-loop time constant is $\tau = RC_a$.

> **Why this matters.** A flat $R = 0.2~\mathrm{K/W}$ applied to every zone gives a 650 m²
> floor plate $UA = 3.3~\mathrm{W/K}$ against a realistic $\approx\!224$, and hence
> $\tau \approx 1300$ h. The identifier in §4.2 then correctly recovers $\tau \approx 10^2$ h
> and is *not* in error. Deriving $R$ from geometry places 93% of zones in the 1–40 h band.

### 5.2 Cooling delivery

Cooling is sized so that at a VAV's nominal flow the zone holds setpoint against its full
nominal load, then scaled by the delivered flow fraction and by the available temperature
lift:

$$
\dot{Q}_{\text{cool}} \;=\; u \cdot \big(\dot{Q}_{\text{int}}^{\text{nom}} + \dot{Q}_{\text{wall}}^{\text{ss}}\big)\cdot\frac{T - T_s}{T_{sp} - T_s},\qquad u = \frac{\dot{V}}{\dot{V}_{\text{nom}}}
$$

clipped at $\dot{Q}_{\text{cool}}\ge 0$ — cold air cannot heat. Normalising by each VAV's own
nominal flow keeps the law correct regardless of how many terminals share an air handler.

### 5.3 Fresh-air load

In a tropical climate the outdoor-air load is the largest single cooling term and is
predominantly *latent* — dehumidification, not sensible cooling:

$$
\dot{Q}_{\text{oa}} \;=\; N\,\dot{v}_{\text{oa}}\,\rho_{\text{air}}\,\Delta h
$$

with $N$ the building occupant count, $\dot{v}_{\text{oa}} = 10$ L·s⁻¹ per person (QCVN
09:2017/BXD; ASHRAE 62.1), $\rho_{\text{air}} = 1.2$ kg/m³, and $\Delta h \approx 55$ kJ/kg
the total enthalpy drop from Ho Chi Minh design outdoor air (33 °C, ~75% RH, ≈88 kJ/kg) to
the supply condition (≈12 °C saturated, ≈33 kJ/kg). At design occupancy this is 2,155 kW of a
3,283 kW total — **66%**. Because it scales with occupants present, it falls away out of
hours exactly as a demand-controlled air handler would allow.

### 5.4 Plant conversion and building load

Plant coefficient of performance degrades with thermal strain, since chillers work against a
higher lift as zones drift above setpoint:

$$
\mathrm{COP} \;=\; \mathrm{clip}\Big(\mathrm{COP}_0 - \beta\,\overline{s},\; 2.2,\; 3.8\Big),\qquad
\overline{s} = \frac{1}{|Z|}\sum_{z\in Z}\max\big(0,\,T_z - T_{sp,z}\big)
$$

with $\mathrm{COP}_0 = 3.6$, $\beta = 0.35$. Total electrical demand is then

$$
P_{\text{bldg}} \;=\; \underbrace{\frac{\sum_z \dot{Q}_{\text{int},z} + \dot{Q}_{\text{oa}}}{\mathrm{COP}}}_{\text{cooling}} \;+\; \underbrace{A_{\text{cond}}\,\rho_B}_{\text{lighting, fans, lifts, pumps}} \;+\; \underbrace{P_{\text{plug}}}_{\text{live}}
$$

where $\rho_B = 9$ W/m² is derived from the case-study end-use shares and $A_{\text{cond}}$ is
conditioned floor area — so the baseline moves when the building does.

### 5.5 Thermal comfort

Discomfort is penalised quadratically in the excess *beyond* the deadband $\delta$, mapped to
a bounded score so that a 0.1 °C overshoot is effectively healthy and a runaway is not:

$$
e_z \;=\; \max\big(0,\; |T_z - T_{sp,z}| - \delta_z\big), \qquad
c_z \;=\; \frac{1}{1 + e_z^2/\sigma_c^2},\qquad \sigma_c = 2.5~^\circ\mathrm{C}
$$

System health charges separately for zones in genuine alarm, because a mean over $|Z|$ zones
hides them — one room at 50 °C across 1,350 zones averages to 99.93%:

$$
H \;=\; \max\!\left(0,\; \frac{100}{|Z|}\sum_{z} c_z \;-\; \min\big(45,\; 12\,|\mathcal{A}|\big)\right),\quad
\mathcal{A} = \{z : T_z > T_{sp,z} + \delta_z + 5\}
$$

### 5.6 Online system identification

Both balances are linear in their parameters, so each is written $y_k = \mathbf{x}_k^\top\boldsymbol{\theta} + \varepsilon_k$:

$$
\underbrace{\frac{dT}{dt}}_{y} = \underbrace{\begin{bmatrix} T_o - \bar T & u(\bar T - T_s) & n & 1\end{bmatrix}}_{\mathbf{x}^\top}
\begin{bmatrix}\theta_0\\\theta_1\\\theta_2\\\theta_3\end{bmatrix},
\qquad
\underbrace{\frac{dC}{dt}}_{y} = \begin{bmatrix} n & C_{\text{out}} - \bar C & 1\end{bmatrix}\begin{bmatrix}\varphi_0\\\varphi_1\\\varphi_2\end{bmatrix}
$$

The recovered coefficients are physical: $\tau = 1/\theta_0$ is the thermal time constant,
$\theta_1$ the cooling authority, $\theta_2$ the per-occupant gain as *this* room experiences
it, and $\varphi_1$ the room's air-change rate in h⁻¹.

**Estimator.** Recursive least squares with forgetting factor $\lambda$:

$$
\mathbf{P}_{k-1}\mathbf{x}_k \;=\; \mathbf{g},\qquad
\iota_k = \mathbf{x}_k^\top \mathbf{g},\qquad
\mathbf{K}_k = \frac{\mathbf{g}}{\lambda + \iota_k}
$$

$$
\boldsymbol{\theta}_k = \boldsymbol{\theta}_{k-1} + \mathbf{K}_k\big(y_k - \mathbf{x}_k^\top\boldsymbol{\theta}_{k-1}\big),
\qquad
\mathbf{P}_k = \frac{1}{\lambda}\Big(\mathbf{P}_{k-1} - \mathbf{K}_k\mathbf{x}_k^\top\mathbf{P}_{k-1}\Big)
$$

with $\lambda = 0.999$, $\mathbf{P}_0 = 50\,\mathbf{I}$. The scalar $\iota_k = \mathbf{x}_k^\top\mathbf{P}_{k-1}\mathbf{x}_k$ is the *information* the sample carries — how far it moves the
regressors into a direction the estimator is still uncertain about.

**Forgetting is conditional.** When $\iota_k < \iota_{\min}$ the update sets $\lambda = 1$.
This is the standard covariance-windup guard: dividing by $\lambda<1$ on an uninformative
sample inflates $\mathbf{P}$ without adding knowledge, and repeated often enough the
estimator becomes arbitrarily sensitive to the next sample — a well-documented failure of
exponential forgetting under poor excitation.

**Midpoint regressors.** The derivative is a finite difference over the sample interval, and
the regressor uses the *midpoint* state $\bar T = \tfrac{1}{2}(T_k + T_{k-1})$ rather than an
endpoint. With endpoint regressors the same measurement noise $\nu_k$ appears in both $y_k$
and $\mathbf{x}_k$, giving $\mathbb{E}[\mathbf{x}_k\varepsilon_k]\neq\mathbf{0}$ — the
errors-in-variables condition — and the least-squares estimate is asymptotically biased
toward zero. The midpoint form makes the shared-noise contribution first-order cancelling.

**Excitation gating.** A sample is admitted only if a *driver* moved or the state is in
genuine transient:

$$
\text{accept} \iff \bigvee_j \frac{|d_{j,k} - d_{j,k-1}|}{s_j} > 1 \quad\lor\quad \Big|\frac{dT}{dt}\Big| \ge \eta
$$

over drivers $d = (T_o, u, n)$ with per-driver scales $s_j$. Gating on the *regressors*
instead admits sensor noise as though it were excitation — the free response of a room after
a step is genuinely informative about $\tau$, whereas a stationary room being jittered by
±0.2 °C of sensor noise is not.

**Uncertainty-weighted ridge.** Under closed-loop control the regressors become collinear —
flow is itself a function of temperature error — so the estimate can drift in the null space
without changing the residual. A leak toward the physical prior $\boldsymbol{\theta}^\ast$ is
applied per coefficient, weighted by that coefficient's *own* remaining uncertainty:

$$
\theta_i \;\leftarrow\; \theta_i + \gamma\,w_i\,(\theta^\ast_i - \theta_i),
\qquad w_i = \mathrm{clip}\!\left(\frac{P_{ii}}{P_0},0,1\right),\qquad \gamma = 0.002
$$

A well-determined coefficient has $P_{ii}\ll P_0$, so $w_i\to0$ and it is left alone; a
poorly excited one is pulled gently back toward physics. An *unweighted* ridge drags
well-identified rooms off their measured values — observed moving a correctly identified
air-change rate from 0.5 to 0.89 h⁻¹.

**Sample spacing.** Observations are taken every $\Delta t_s = 300$ s. This is a
signal-to-noise choice, not a responsiveness one. For sensor noise $\sigma_\nu$, the
finite-difference derivative has standard error

$$
\sigma_{\dot T} \;=\; \frac{\sqrt{2}\,\sigma_\nu}{\Delta t_s}
$$

An SHT30 at $\sigma_\nu \approx 0.2$ °C differenced over 30 s gives $\pm17$ °C/h against a
true rate of a few °C/h; over 300 s it falls to $\pm1.7$ °C/h. Maturity requires $k \ge 36$
accepted samples — three simulated hours, on the principle that a system whose time constant
is measured in hours cannot be characterised in minutes.

### 5.7 Setback depth as a solved quantity

This is where the identified model does work a schedule cannot. With the room empty ($n=0$)
and cooling at full flow ($u=1$), the thermal balance is first-order with pole

$$
k \;=\; \theta_0 - \theta_1
$$

and equilibrium

$$
T_\infty \;=\; \frac{\theta_0 T_o - \theta_1 T_s + \theta_3}{k}
$$

If $T_\infty \ge T_{sp}$ the room cannot reach setpoint even at full cooling; it has no
recovery margin and **the setback is refused**. Otherwise, integrating the free response
backwards over the recovery budget $t_r$ gives the temperature from which the room can just
return in time:

$$
T_{sb} \;=\; T_\infty + (T_{sp} - T_\infty)\,e^{\,k\,t_r}
$$

$$
\boxed{\;\Delta_{\text{setback}} \;=\; \mathrm{clip}\Big(T_{sb} - T_{sp} - \epsilon,\; \Delta_{\min},\; \Delta_{\max}\Big)\;}
$$

with safety margin $\epsilon$ and $t_r = 1800$ s. A light, responsive room earns a deep
setback; a heavy one earns a shallow one; a room that cannot recover earns none. **Comfort
cannot fail silently**, because the bound is derived from the room's own measured recovery
capability rather than assumed — which is precisely the failure reported in §2.3.

### 5.8 Baseline anomaly scoring

Independently of the physical models, each $(\text{zone},\text{metric},\text{hour})$ triple
carries an online mean and variance updated by Welford's algorithm, and observations are
scored in standard deviations:

$$
\mu_k = \mu_{k-1} + \frac{x_k-\mu_{k-1}}{k},\qquad
M_{2,k} = M_{2,k-1} + (x_k-\mu_{k-1})(x_k-\mu_k),\qquad
\sigma_k = \sqrt{\frac{M_{2,k}}{k-1}}
$$

$$
z \;=\; \frac{x - \mu}{\max(\sigma,\sigma_{\min})}
$$

The floor $\sigma_{\min}$ prevents a series that has been quiet from generating enormous
scores on its first small deviation. This answers "abnormal *for this room, at this hour*",
which a fixed threshold such as $\mathrm{CO_2} > 1000$ cannot express.

### 5.9 Energy, intensity and carbon

Avoided cooling from a setback is the envelope load the zone no longer holds down, bounded by
the available outdoor lift — it correctly vanishes when there is no lift to exploit:

$$
\dot{Q}_{\text{saved}} \;=\; \frac{1}{R_i + R_o}\,\min\big(\Delta_{\text{setback}},\; T_o - T_{sp}\big),\qquad \text{for } T_o > T_{sp}
$$

Annual energy intensity is defined on the **mean** load, since $\int_0^{8760} P\,dt = \bar P \cdot 8760$ exactly:

$$
\mathrm{EUI} \;=\; \frac{\bar P \cdot 8760}{A_{\text{floor}}},\qquad
\bar P = \frac{\int P\,dt}{\int dt} \;\approx\; \frac{\sum_k \tfrac{1}{2}(P_k + P_{k-1})\,\Delta t_k}{\sum_k \Delta t_k}
$$

with intervals longer than 5 minutes excluded as unobserved rather than assumed constant.
Substituting an instantaneous $P$ for $\bar P$ is not an approximation but a different
quantity: an office at 09:00 with 3,000 people in it sits far above its own annual mean, and
the resulting figure over-reads by roughly 3×.

Operational carbon follows the national grid factor $\epsilon_g = 0.6766$ tCO₂/MWh:

$$
E_{\mathrm{CO_2}} \;=\; \frac{\bar P\,[\mathrm{kW}] \cdot 8760 \cdot \epsilon_g}{1000}~\mathrm{tCO_2/yr}
$$

---

## 6. Implementation

### 6.1 Building fixture

The bundled building is a 15-storey, 39,777 m², 735-zone office derived from a real floor
plan by DeepFloorplan segmentation. The segmenter locates room *boundaries* well and names
their *function* poorly: it labelled 555 closets averaging 4.2 m² as server rooms at 85 kW
each — 14,768 W/m², and 86% of the building's connected load.

A post-processing step (`tools/officeize_fixture.py`) keeps every polygon and re-derives
programme and physics from geometry against the library: internal gain from published power
densities, air capacitance from each room's actual volume, and envelope resistance from each
zone's own façade, roof and partition area. Calibration target is the measured cohort [2].

### 6.2 Edge hardware

Nodes are ESP32-WROOM-32 with SHT30 temperature/humidity (±0.2 °C), Ai-Thinker Rd-03 24 GHz
radar presence (which holds a *stationary* occupant), ASAIR ACD1200 NDIR CO₂, an SCT-013
split-core current transformer for plug metering, opto-isolated relays, and an infrared
emitter driving vendor AC protocols. Total ≈ 590,000 VND per standard node at Vietnamese
retail. Three further sensors are implemented in firmware to replace remaining assumptions
with measurements: a DS18B20 supply-air probe, a second current clamp on the air
conditioner's own supply, and a BH1750 illuminance sensor.

## 7. Results

We separate what is verified from what is not, because the distinction is the point.

### 7.1 Verified

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

### 7.2 Not demonstrated

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

## 8. Limitations

- **No measurement and verification baseline.** Savings are a model counterfactual, never
  compared against a measured baseline period under IPMVP or equivalent. This is the single
  largest gap between the projection above and a defensible claim.
- **No demand-controlled ventilation loop.** CO₂ is measured and air-change rate identified,
  but nothing acts on either. Ventilation is 15.3% of the case-study building's energy and is
  deliberately excluded from the projection in §7.2 for this reason.
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

## 9. Conclusion

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
