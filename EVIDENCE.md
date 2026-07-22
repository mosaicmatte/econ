# The case for ECON

What the case studies actually measured, what a conventional BMS provably does not do about
it, and which part of this repository answers each gap.

Every number below is quoted from one of the five sources; nothing here is a vendor claim.
Where a figure is an assumption rather than a measurement, it is labelled as one.

---

## The sources

| # | Source | What it measures |
|---|---|---|
| **A** | Nguyen Duc Luong, Nguyen Thi Hue, Nguyen Huy Tien, Nguyen Van Duy, Hoang Xuan Hoa (2025). *Assessing energy consumption and operational carbon emission: A case study of office building in Hanoi.* Journal of Materials and Construction 15(02). [DOI](https://doi.org/10.54772/jomc.v15i02.1190) | One 45-storey, 117,000 m² Hanoi office tower, metered Sep 2024 – May 2025, broken down by end use |
| **B** | Hoang Tuan Viet, Nguyen Duy Dong, Nguyen My Anh, Nguyen Duc Luong, Tran Ngoc Quang, Mac Van Dat, Joseph J. Deringer (2021). *A study of energy consumption for office buildings in Vietnam.* Proc. Int. Conf. Evolving Cities, 63–70. [DOI](https://doi.org/10.55066/proc-icec.2021.19) | 57 commercial and government offices in Hanoi and HCMC (USAID Vietnam Clean Energy Program survey) |
| **C** | Li Meng, Rita Yi Man Li, William Finocchiaro, Alemu Alemu (2023). *Integrating Building Management Systems (BMS) and BIM to improve HVAC efficiency in Darwin.* EBIMCS 2023, ACM. [DOI](https://doi.org/10.1145/3644479.3644494) | Field tests of three BMS control strategies on a real tropical office building, wet season vs dry season |
| **D** | Decision **896/QĐ-TTg** — National Strategy on Climate Change to 2050 | The binding national targets |
| **E** | Bach Khoa Innovation × Future Impact Challenge, Round 1 brief | The 10–20 % reduction target and the judging criteria |

---

## Four findings, and what each one costs a conventional BMS

### 1. The building in Source A already has a BMS. Its single largest load is the one the BMS does not touch.

The Hanoi tower is not an unmanaged building. It has water-cooled chillers, high-performance
LEDs with daylight sensors, and *"a building management system (BMS) … to control and manage
all systems in the building including chillers and cooling towers of air conditioning,
lighting, pumps, ventilation fans, elevators"* (§2.1).

With all of that running, the measured end-use split was:

| End use | Share of energy |
|---|---|
| **Plug loads** | **26.4 %** |
| Air-conditioning | 25.1 % |
| Outdoor lighting | 16.1 % |
| Ventilation | 15.3 % |
| Indoor lighting | 9.1 % |
| Elevators | 7.8 % |
| Domestic water pumps | 0.2 % |

The largest end use in a fully BMS-managed tower is the one category a BMS has no visibility
of and no actuator on. A BMS controls *plant* — chillers, towers, pumps, fans, lifts. Plug
load sits downstream of the socket circuit: unmetered, unswitched, and invisible on the
operator's screen. The paper's own recommendation is *"automated plug load controls,
awareness programs, and shutdown protocols"* — none of which a plant-level BMS can provide.

### 2. Control strategies do not transfer between seasons — one of them made things worse.

Source C field-tested three strategies on a tropical office building and reported each by
season:

| Strategy | Wet season | Dry season |
|---|---|---|
| Space temperature setpoint reset | **+8 %** | **+11 %** |
| Chilled water delivery temperature optimisation | +5 % | +7 % |
| **Condenser water delivery temperature optimisation** | **−7 %** | +3 % |

The same strategy, unchanged, **lost 7 % in one season and gained 3 % in the other.** A rule
commissioned once and left alone is not merely suboptimal for part of the year — it can be
actively negative, and nothing in the BMS will say so.

The paper is equally direct about geography: a Canberra retrofit saved 60 % of annual energy
and 70 % of emissions, but the authors note that building *"has a drastically different
climate to Darwin and no similar current studies have been conducted in a hot humid tropical
climate."* Vietnam is hot and humid. Strategies validated in temperate climates arrive here
unvalidated.

### 3. Comfort was broken by the very strategies that saved energy — and nobody would have known.

From the Source C abstract: *"the thermal comfort of the occupants was compromised during the
wet season when both these strategies were implemented."*

A plant-optimising BMS measures the plant. It does not hold a per-room model of whether a
given room can still recover to setpoint, so it cannot tell the difference between a saving
and a comfort failure until someone complains. Source E makes comfort a scored requirement,
not a nice-to-have: the solution must reduce energy *"while maintaining or improving occupant
comfort."*

### 4. The BMS is frequently a time clock.

Source C, citing the literature: *"the BMS is not used to its full potential in many
buildings, performing little more than time clock functions"*, and such systems are *"often
set as fixed-temperature systems without considering daily weather changes, wasting energy
and causing human discomfort."*

A schedule cannot know that a meeting room emptied twenty minutes ago, and a fixed setpoint
cannot know that today is 6 °C cooler than yesterday. Source B measured exactly this
sensitivity across 57 Vietnamese offices: monthly electricity intensity *tracks ambient air
temperature*, and mean EUI is higher in HCMC (116.4 kWh/m²·yr) than Hanoi (105.9) precisely
because the climate is hotter year-round. In Hanoi, commercial offices spend about **2.5×**
what government offices spend on air-conditioning (≈41.4 kWh/m²·yr).

---

## What this repository does about each gap

| Gap the evidence shows | What ECON does | Where |
|---|---|---|
| Plug load is the largest end use and the BMS cannot see it | Measures real socket-circuit watts with a clamp CT and publishes `plugW`; the APLC after-hours sweep switches a non-critical socket circuit | `edge/esp32` + `server/simulation/plugs.go` |
| A strategy that works in the dry season loses 7 % in the wet | Nothing is a fixed rule. Each room's thermal and CO₂ balances are identified online by recursive least squares with a forgetting factor, so the coefficients follow the season instead of being commissioned against it | `server/simulation/dynamics.go` |
| Fixed thresholds cannot express "normal for this room, at this hour" | Per-(zone, metric, hour) baselines learned from the building's own data; anomalies are scored in σ against that distribution | `server/simulation/baselines.go` |
| A time clock cannot know a room is empty | Measured presence — 24 GHz radar that holds a *stationary* person, plus a YOLO/ByteTrack head count on the same MQTT contract | `edge/esp32`, `ai_modules/branch_a_occupancy` |
| Saving energy silently broke comfort | The vacancy setback is **solved, not assumed**: `SetbackCeiling()` returns how far a room may drift given its own identified recovery capability, and refuses a setback the room cannot recover from | `server/simulation/dynamics.go` |
| No per-room model, so no warning before a room fails | Identified time constant, cooling authority and measured air-change rate give ETA predictions and "ventilation will not keep up with occupancy" warnings before the room is uncomfortable | `server/simulation/recommend.go` |
| Savings are credited whether or not the command reached a machine | `acReal` reports whether setpoints actually reach an air conditioner; `tempReal` separates measured from modelled; the dashboard reports which rooms are identified and which are still learning | `server/mqtt.go`, `server/simulation/engine.go` |

Source A's own closing sentence is the specification: *"Future work should focus on real-time
monitoring, dynamic modeling, and evaluation of retrofit scenarios."* That is what this
system is.

---

## Expected saving, computed against Source A's measured shares

The end-use shares are measured. The **reduction fractions are assumptions** and are the
part of this table to argue with:

| End use | Measured share | ECON mechanism | Assumed reduction | Building saving |
|---|---|---|---|---|
| Plug loads | 26.4 % | Metered sweep of non-critical sockets outside occupied hours | 25 % | **6.6 %** |
| Air-conditioning | 25.1 % | Per-room learned setback, bounded by identified recovery | 12 % | **3.0 %** |
| Ventilation | 15.3 % | Demand-controlled ventilation on measured CO₂ and identified ACH | 15 % | **2.3 %** |
| Indoor lighting | 9.1 % | Presence-driven switching | 25 % | **2.3 %** |
| Outdoor lighting, elevators, pumps | 24.1 % | not addressed | — | — |
| | | | **Total** | **≈ 14.2 %** |

That lands inside the 10–20 % band Source E asks for, and it is derived from the metered
end-use split of a real Vietnamese office tower rather than from a product datasheet.

The air-conditioning assumption is the one with independent support: Source C *measured*
8–11 % from setpoint reset alone on central plant. 12 % from per-room control that also knows
when a room is empty is the same order of magnitude, not a leap.

**Carbon.** Vietnam's grid emission factor is **0.6766 tCO₂/MWh** (Source A, 2022 national
figure), and with a single grid factor the CO₂ saving is proportional to the electricity
saving — so ≈14.2 % of operational carbon as well. At that building's peak month (219.4 t CO₂,
October 2024) a 14.2 % cut is **≈31 t CO₂ avoided in one month, in one building**.

> **A discrepancy in Source A worth noting.** The paper reports one set of shares for energy
> (§3.2) and a different set for CO₂ (§3.3) — indoor lighting 9.1 % of energy but 16 % of
> emissions, outdoor lighting 16.1 % of energy but 0.4 % of emissions. Under a *single* grid
> emission factor, carbon share must equal energy share; the two figures cannot both be
> right, and the pattern suggests the indoor/outdoor labels were transposed in one of them.
> The energy shares are used throughout this document because they are the internally
> consistent set.

---

## Why this matters beyond one building

- Buildings are **37–40 % of Vietnam's total final energy consumption** (Source A, §1), and
  coal and gas still supply **over 60 %** of the grid — so a kWh saved in a Vietnamese office
  carries more carbon than the same kWh saved in a temperate country.
- Decision 896/QĐ-TTg requires **−43.5 % national GHG against BAU by 2030**, with the energy
  sector cutting **32.6 %** to no more than 457 Mt CO₂e, and net zero by 2050.
- The same decision makes reduction **mandatory** for any facility emitting **≥ 2,000 t CO₂e
  per year**. Source A's building emitted between 128.5 and 219.4 t CO₂ per month over the
  nine months studied; even the low bound sustained year-round is ~1,542 t, and the mid-range
  is ~2,088 t. The three hottest months (June–August) were *excluded* from that study, so the
  true annual figure sits at the upper end. **A single Hanoi office tower is at or over the
  threshold where emission reduction stops being voluntary.**

---

## What is demonstrated and what is not

Stated plainly, because the difference is the whole point of the honesty discipline in this
codebase:

**Demonstrated.** The identification maths works against ground truth — the test suite
recovers a known room's time constant and cooling authority to within 15 %, and its air-change
rate from a simulated CO₂ balance. Two physical edge nodes publish real telemetry into the
twin. Real vendor IR frames reach a real air conditioner, and the twin reports `acReal:false`
for any zone where they do not.

**Not demonstrated.** ECON has not yet run for a year in an occupied building, so the 14.2 %
is a projection from measured shares, not a measured outcome. Most zones in the current
digital twin are simulated; the dashboard says which are which, and that distinction is
enforced in code rather than in a footnote — a room without a live NDIR is never used to
train the CO₂ balance, and a placeholder temperature never pins the physics.

The honest KPI set for a pilot, in the order they can actually be evidenced:

1. **Rooms identified** — how many have a converged physical model (reported live).
2. **kWh avoided on the plug circuit** — directly metered by the clamp, before/after the sweep.
3. **Setback minutes delivered without a comfort excursion** — the setback is only credited
   when the room's own model says it can recover, so this is measurable rather than assumed.
4. **CO₂ ppm held below 1000** during occupied hours — measured by NDIR, not modelled.
5. **tCO₂ avoided** = kWh avoided × 0.6766 / 1000.
