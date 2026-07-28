# House pilot — roadmap and wiring

_Compiled 2026-07-26. The test bed is the **ground floor of a nhà ống (tube house) in Ho Chi
Minh City**, chosen because its electrical components are reachable at working height._

This is the *current* plan. [ROADMAP.md](ROADMAP.md) is the standing gap ledger for the
system as a whole; this file is the pilot that actually gets hardware onto a real building.
[../edge/WIRING.md](../edge/WIRING.md) remains the full wiring reference — what is here is
the subset that applies to this house, plus what the survey changed.

---

## Why the pilot exists

Until 2026-07-26 the twin modelled a **synthetic 735-zone office tower, 39,776 m²**. The
building being instrumented is **5 rooms, 67.6 m²**. Everything the engine had learned —
53,778 baseline buckets, 5 identified room models — described a place that does not exist.

The scan fixed that. `tools/housify_fixture.py` turns a Polycam LiDAR export into the real
fixture, and the engine now boots on it.

| Zone | Programme | Area | τ (prior) | Solar | Notes |
|---|---|---|---|---|---|
| Kitchen & rear service | `kitchen` | 27.2 m² | 0.77 h | 0.27 | rear service space, 1 window |
| Office | `home-office` | 21.9 m² | 1.54 h | 0.80 | east end, **3 windows**, has the AC |
| Living room | `living` | 9.3 m² | 0.87 h | 0.00 | interior |
| Passage | `circulation` | 5.4 m² | 0.84 h | 0.27 | 3.5 m ceiling — stair core |
| Bathroom | `bathroom` | 3.9 m² | 0.62 h | 0.00 | unconditioned |

Envelope 13.56 × 5.51 m, ceilings 2.8 m, 9.5 m² of glazing, 5 doors.
**Measured** by the scan: polygons, areas, wall areas, volumes, ceiling heights, door and
window positions. **Inferred**: which programme each room is (Polycam labels only two of
five) and the glazing split. τ values are *priors* — rule 4 says the RLS identification
supersedes them once each room has ~3 h of real data.

Regenerate with:

```bash
python3 tools/housify_fixture.py --scan "<Polycam export dir>" --write
```

Output goes to `server/data/building-data.local.json`, which is gitignored — a `git pull`
cannot overwrite it, and a collaborator without the scan still boots the repo default.

---

## What the electrical survey changed

Twenty photographs of the floor's wiring, summarised into the decisions they force:

| Observation | Consequence |
|---|---|
| Main board (Panasonic BD-63R **C50**, 30 A ceramic fuse, SVP-912 40 A protector reading 224 V) is **above a doorway at ladder height** | **Do not instrument the main.** Nothing we measure is worth opening a utility enclosure on a ladder |
| **Local breakers at working height** in at least four places, each above a socket outlet | This is where metering goes. Reachable, and gives *per-area* disaggregation |
| Circuit cable is **TCVN 6610 300/500 V 2 × 0.75 mm²** | ~6–10 A circuits. A 100 A clamp is the wrong instrument by an order of magnitude |
| **Every socket is 2-pin. No earth anywhere. No RCD/ELCB seen** | **Drop in-wall SSR switching.** Actuation moves to plug level, inside a manufactured enclosure |
| **Unenclosed live splices** in ≥3 places (conduit corner, AC lineset penetration, beside the router) | Do not disturb. Flag to an electrician |
| **Brown radial scorching** around one switch + socket | ⚠️ Looks like arcing at a loose terminal. **Have this looked at before anything else** |
| VNPT router sits high in a ceiling corner | WiFi coverage is fine anywhere on this floor |

> ### ⚠️ Before any hardware goes in
> The scorched outlet and the exposed splices are pre-existing conditions of the house, not
> consequences of this project, but they are the reason the project should not add load or
> disturb wiring until a qualified electrician has been through. **No earth and no RCD on a
> 50 A supply feeding 0.75 mm² circuits** is the single largest safety gap on this floor.

---

## Roadmap

### Phase 0 — done

- ✅ Real fixture from the Polycam scan (`housify_fixture.py`), engine boots on it
- ✅ Residential programmes in `programme-library.json` (kitchen, living, bedroom,
  home-office, bathroom, circulation) with the office-vs-house differences documented
- ✅ Runtime node config over MQTT + NVS — calibration without reflashing
- ✅ `supplyC` / `acW` / `lux` wired to the coefficients they displace
- ✅ Both forecasters comparable side by side (`/api/forecast/compare`)
- ✅ Local-override layer so a `git pull` cannot delete a deployment's building

### Phase 1 — measure one appliance honestly *(next)*

**Goal: one true watt-hour reading from this house.** Not a circuit, not the main — one
appliance, on the bench, unplugged while wiring.

1. **Buy the PZEM-004T** ([hshop, 225.000₫](https://hshop.vn/mach-do-ap-dong-cong-suat-nang-luong-ac-100a-giao-tiep-uart)).
   Cheapest verified source; caka is out of stock and Shopee is 275–315k.
2. **Wire it into a short extension lead** — see the wiring section below.
3. **Add a `USE_PZEM` firmware path** publishing `plugW` (**true active power**),
   `plugV`, `plugPf`, `plugKwh`.
4. **Validate against a reference** — a 199k Tuya metering plug used purely as ground truth,
   not as part of the system.

> **Why this replaces the SCT-013 plan.** `plugW = amps × assumed_voltage` is **apparent
> power (VA)**, not watts. Every load in this house that matters — RO purifier pump, AC
> compressor, fans, cheap LED drivers — runs at power factor 0.5–0.9, so the CT approach
> overstates them by 10–50%, and that error propagates into `buildingLoadMw`, the LSTM's
> training target, and every savings claim. Publishing VA on a channel named `plugW` is the
> same class of error as fabricating a measurement.

### Phase 2 — make one room real

5. Bind a node to the **Office** zone (the AC is there, it has the glazing, and it is where
   someone actually sits). Fix the node→zone binding to the real `zone-office-lvl1`.
6. **SHT30 soldered to header pins** — 82.7 % arrival is mechanical, not code.
7. **Verify the mmWave**: the Rd-03 reported occupancy `1.00` across 769 messages and never
   once `0`. Meter OT2 directly (0 V empty, 3.3 V on movement) before believing it. If it is
   dead, [LD2420 57.000₫ / LD2410S 105.000₫](https://hshop.vn) are back in stock.
8. Let the RLS identify the Office for ~3 h and compare the identified τ against the 1.54 h
   prior. **This is the first real test of the whole thesis.**

### Phase 3 — actuation, safely

9. **Plug-level only.** A switched socket the node controls; no in-wall SSR, no mains
   termination by us, given no earth and no RCD.
10. Re-point `plugs.go` sweep logic at the real appliance inventory from the survey.

### Phase 4 — honest claims

11. **Re-scope the evidence base.** The Hanoi 45-storey office study (26.4 % plug load,
    109.6 kWh/m²·yr) describes a BMS-managed commercial tower. This is a house with no BMS.
    Rule 3 forbids crediting a saving from a mechanism that does not apply here.
12. Find Vietnamese **residential** energy data, or state plainly that the pilot has none yet.
13. Cut the persistence rate — 1 Hz × 735 zones was already indefensible; at 5 zones it
    should fall out automatically, but verify.

---

## Wiring — the house pilot bench

### Node placement

Put the node in the **Office**. It has the AC, the glazing, an occupant, and a socket at
working height. WiFi from the ceiling-corner VNPT router covers the whole floor.

### Build sheet

| Item | Flag | Where | Note |
|---|---|---|---|
| ESP32 DevKit v1 | — | Office | powered from a 5 V adapter at a wall socket |
| SHT30 | `-DUSE_SHT30=1` | Office, ~1.1 m, away from the AC discharge | **solder header pins** |
| Rd-03 / LD2420 | `-DUSE_MMWAVE=1` | facing the desk | verify OT2 with a meter first |
| PZEM-004T | `-DUSE_PZEM=1` *(to write)* | inline in an extension lead | UART, not ADC |
| ACD1200 CO₂ | `-DUSE_CO2=1` | same I²C bus | needs the level shifter — 5 V pull-ups |

Not in this build: SSR (no earth), IR AC control (add once the room is trusted),
SCT-013 + analog front end (**superseded by the PZEM**).

### PZEM-004T wiring

```
     ┌──────────── extension lead, IN (from wall socket) ────────────┐
     │  L ──────────┬──────────────────────► PZEM  L-in             │
     │  N ──────────┼──────────┬───────────► PZEM  N-in             │
     └──────────────┼──────────┼──────────────────────────────────── ┘
                    │          │
              (L passes through the split CT jaw — one conductor only)
                    │          │
     ┌──────────────┴──────────┴──── extension lead, OUT (to load) ──┐
     │  L, N to the socket the appliance plugs into                  │
     └───────────────────────────────────────────────────────────────┘

     PZEM 5 V side          ESP32
     ────────────────────────────────
     VCC  ──────────────►   5 V (VIN)
     GND  ──────────────►   GND
     TX   ──────────────►   GPIO16   (RX2)
     RX   ──────────────►   GPIO17   (TX2)
```

**The 5 V side is fully isolated from mains inside the PZEM.** GPIO16/17 are UART2 and clash
with nothing already in use — I²C is 21/22, IR 19, relays 23/25, clamps 34/35, mmWave 18.

Rules for this build:

1. **Do the mains work once, on the bench, unplugged, then never again.** Everything after
   that is USB-side.
2. **The CT jaw goes around the live conductor only.** Around both L and N it reads ≈ 0 A
   because the currents cancel — the most common first-day failure, and it looks exactly
   like a dead sensor.
3. **Enclose it.** Dust, a semi-outdoor environment and 224 V. A printed or bought box, not
   bare on a breadboard.
4. **Calibrate at runtime, not at compile time** — `plugMainsV` is now measured by the PZEM
   itself, so the assumed-voltage error disappears entirely.

### Terminating the sensors

Stranded leads in breadboard clips are what cost the SHT30 its 17 % of missing samples. Use
a **CH-2 spring clamp** (2.000₫, caka) to join stranded to solid-core, and put only the solid
jumper in the breadboard — it costs zero breadboard holes. Full reasoning:
[WIRING.md §5](../edge/WIRING.md).

Keep the analog front end, if you ever build one, on its own **SYB-170 mini board**
(6.000₫) — an ESP32 DevKit leaves at most one or two free rows on one side of a breadboard
and none on the other.

---

## Shopping list for Phase 1

| Item | Source | Price |
|---|---|---|
| PZEM-004T 100 A UART | hshop `HS…` | **225.000₫** |
| CH-2 spring clamps ×4 | caka | 8.000₫ |
| SYB-170 mini breadboard | caka | 6.000₫ |
| Extension lead (zip cord, 2-pin) | any hardware shop | ~30.000₫ |
| Enclosure | — | — |
| *(optional)* Tuya 16 A metering plug — **reference meter only** | Shopee | 199.000₫ |

≈ **270.000₫** for the phase, or 470k with the reference meter.

Deliberately **not** buying yet: more sensors. There is already more instrument than this
building has model — and adding a fourth sensor type to a twin that has only just started
describing the right house does not get the pilot further.
