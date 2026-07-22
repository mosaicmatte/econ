# ECON Edge Hardware — Shopping List

What to actually buy to put ECON on real hardware, organised by how far you want to go.
Every part here is referenced by the firmware in `edge/esp32/src/main.cpp`; nothing is
aspirational. Pin assignments and circuits are in [WIRING.md](WIRING.md).

> **Prices are indicative, in VND, for Ho Chi Minh City (hshop.vn / Nhật Tảo) as a planning
> figure only — check them at purchase time.** They are here to size a budget, not to quote
> one. Availability moves faster than this document.

> **Sourcing reality check.** DHT22, MH-Z19 and Sensirion SCD4x are generally *not* stocked
> in Vietnam. The parts below are chosen because they are: SHT30 instead of DHT22, ASAIR
> ACD1200 instead of MH-Z19. If a listed part is out, the "if unavailable" column is a
> like-for-like substitute the firmware already supports.

---

## Pick a build

| Build | What it demonstrates | Parts | ≈ VND / node |
|---|---|---|---|
| **A. Bare demo** | The full software loop with zero wiring — capacitive touch presence on GPIO32, simulated temperature (honestly flagged `tempReal:false`) | ESP32 only | ~180k |
| **B. Standard office node** | Real temperature, real presence including people sitting still, real AC control | A + SHT30 + mmWave + IR emitter + lighting relay | ~500k |
| **C. Fully instrumented** | Everything in B plus measured CO₂ and ventilation identification | B + ACD1200 + level shifter | ~1,150k |
| **D. Plug-load node (APLC)** | Adds the load a conventional BMS neither meters nor controls | C + SCT-013 clamp + analog front end + 2nd relay | ~1,400k |

Build **B** is the recommended starting point for a real pilot: it is the cheapest
configuration where every number on the dashboard is measured and every command reaches a
machine. Build **D** is what the plug-load case study (26.4% of office energy) needs.

---

## 1. Core — every node

| # | Part | Qty | Why | If unavailable | ≈ VND |
|---|---|---|---|---|---|
| 1 | **ESP32 DevKit v1** (30-pin, CP2102 or CH340) | 1 | The node. WiFi + enough GPIO + two ADCs | ESP32-WROOM-32 NodeMCU — same pinout | 130–180k |
| 2 | Micro-USB cable (**data**, not charge-only) | 1 | Flashing + power | — | 20–40k |
| 3 | 5 V / 2 A USB power supply | 1 | Relays + radar draw more than a laptop port likes | Any 5 V ≥1 A | 50–80k |
| 4 | Breadboard 830-point + jumper kit (M-M, M-F) | 1 | Prototyping | Perfboard + solder for permanent installs | 60–100k |

> The ESP32's 3.3 V regulator is good for roughly 500 mA. That covers the board, the
> sensors and the *coils* of two opto-isolated relay boards — but feed relay boards and the
> radar from **5 V (VIN)**, not 3V3. See the power budget in [WIRING.md](WIRING.md).

## 2. Sensing

| # | Part | Qty | Firmware flag | Notes | ≈ VND |
|---|---|---|---|---|---|
| 5 | **SHT30 module** (I²C, addr 0x44) | 1 | `-DUSE_SHT30=1` | ±0.2 °C. **Buy this, not a DHT11** — a DHT11's ±2 °C is meaningless against a 2 °C control deadband, and the twin's AFDD residual would be measuring the sensor | 80–120k |
| 6 | **HLK-LD2410C** 24 GHz presence radar | 1 | `-DUSE_MMWAVE=1` | Holds presence for a person sitting still. A PIR drops them and the lights go out on a full meeting | 150–220k |
| 6b | *(alt)* Ai-Thinker **Rd-03** / Rd-03_V2 | 1 | same | Stocked when HLK is out; same "output high when sensing" contract, 3.3 V supply | 130–200k |
| 7 | **ASAIR ACD1200** NDIR CO₂ (I²C, addr 0x2A) | 1 | `-DUSE_CO2=1` | 400–5000 ppm. Required for the ventilation/air-change identification | 550–750k |
| 8 | **Bidirectional I²C level shifter** (4-ch, BSS138) | 1 | with #7 | **Not optional with the ACD1200** — its I²C lines are pulled to 5 V internally and the ESP32 is not 5 V tolerant | 25–40k |
| 9 | *(optional)* PIR HC-SR501 | 1 | `-DUSE_PIR=1` | Cheap motion presence; OR-ed with the radar if both fitted | 30–50k |
| 10 | *(fallback)* DHT11 | 1 | `-DUSE_DHT=1 -DDHT_TYPE=DHT11` | Only if SHT30 is unavailable — see #5 | 25–40k |

## 3. Actuation

| # | Part | Qty | Firmware flag | Notes | ≈ VND |
|---|---|---|---|---|---|
| 11 | **Opto-isolated relay module, 1-ch, 5 V, active-HIGH** | 1 | default | Zone lighting on GPIO23. Check the coil voltage is 5 V and the module is opto-isolated | 30–50k |
| 12 | **Opto-isolated relay module, 1-ch, 5 V, active-HIGH** | 1 | `-DUSE_PLUG=1` | Non-critical socket circuit on GPIO25. Boots **energized** — a crashed node must never dark-kill a live socket | 30–50k |
| 13 | **IR LED 940 nm, 5 mm** | 1–2 | `-DUSE_IR_AC=1` | Drives the split unit. Two in series widens coverage in a large room | 5–10k |
| 14 | **NPN transistor** 2N2222 / S8050 / PN2222 | 1 | with #13 | **Required.** A GPIO sources ~12 mA; an IR LED needs ~100 mA of pulse current for useful range. Driving the LED straight off the pin gives you a metre, if that | 3–5k |
| 15 | Resistors: **1 kΩ** (transistor base), **10 Ω** ¼ W (LED current) | 1 ea | with #13 | See the driver circuit in WIRING.md | 1k ea |

> **Which IR protocol?** Build with `-DUSE_IR_AC=1 -DIR_AC_PROTOCOL=COOLIX` first — it
> covers many budget/OEM splits. For Daikin / Panasonic / Mitsubishi / LG / Samsung /
> Toshiba / Gree (Casper and Nagakawa are often Gree-compatible) set the matching protocol;
> all eight are verified to compile. If the unit ignores you, capture its handset with
> IRremoteESP8266's `IRrecvDumpV3` example and use whatever it decodes to.

## 4. Plug-load metering (build D)

| # | Part | Qty | Notes | ≈ VND |
|---|---|---|---|---|
| 16 | **SCT-013-000** split-core CT (100 A : 50 mA) | 1 | Current-output variant. Needs the burden resistor below | 150–250k |
| 16b | *(alt)* **SCT-013-030** (30 A : 1 V) | 1 | Voltage-output — **skip the burden resistor** and build `-DPLUG_CAL_A_PER_V=30.0` | 180–280k |
| 17 | **33 Ω** ¼ W resistor (burden) | 1 | Only for the **-000**. Sets `-DPLUG_CAL_A_PER_V=60.6` | 1k |
| 18 | **10 kΩ** ¼ W resistors (bias divider) | 2 | Lifts the AC signal to 1.65 V so the ADC sees the negative half | 2k |
| 19 | **10 µF** electrolytic capacitor | 1 | Stiffens the bias node | 2k |
| 20 | 3.5 mm mono jack socket | 1 | For the CT's plug | 5–10k |

> ⚠️ **The clamp goes around ONE conductor — the live wire only.** Clamped around a whole
> two-core cord it reads ~zero, because the live and neutral currents cancel. This means
> opening the circuit's enclosure. **In Vietnam, have a licensed electrician do the mains
> side.** The CT itself is non-contact and safe; getting to the single conductor is not.

## 5. Gateway (one per site, not per node)

| # | Part | Qty | Why | ≈ VND |
|---|---|---|---|---|
| 21 | **Raspberry Pi 4** (2 GB+) or Pi 5 | 1 | Hosts the Mosquitto broker and `gateway.py`, the failsafe rules engine that keeps a vacant zone from being left lit and cooled when the Go engine is unreachable | 1,500–2,500k |
| 22 | microSD 32 GB (A1/A2) | 1 | — | 120–200k |
| 23 | Official Pi PSU | 1 | Under-powering a Pi 4 causes exactly the kind of intermittent fault you will waste a day on | 250–350k |

Any always-on Linux box works for a pilot; the Pi is the deployable form factor.

## 6. Optional second node type

| # | Part | Qty | Why | ≈ VND |
|---|---|---|---|---|
| 24 | **Raspberry Pi Pico** (or Pico W) | 1 | The `edge/pico` firmware: RP2040 internal temperature, BOOTSEL/GP16 presence, onboard LED as the lighting actuator, 8 s watchdog. A plain Pico speaks JSON over USB via `bridge.py`; a **Pico W** joins MQTT directly | 120–250k |

---

## Worked bundle — a 4-zone pilot

One instrumented floor: three standard office nodes plus one plug-load node, on one gateway.

| Item | Qty | ≈ VND |
|---|---|---|
| ESP32 DevKit v1 + cable + PSU | 4 | ~1,000k |
| SHT30 | 4 | ~400k |
| LD2410C radar | 4 | ~700k |
| IR LED + transistor + resistors | 4 | ~40k |
| Relay module (lighting) | 4 | ~160k |
| ACD1200 + level shifter | 2 | ~1,400k |
| SCT-013-000 + analog front end + 2nd relay | 1 | ~300k |
| Raspberry Pi 4 + SD + PSU | 1 | ~2,300k |
| Breadboards, jumpers, enclosures | — | ~400k |
| **Total** | | **≈ 6.7M VND** |

CO₂ is the expensive line. Fit the ACD1200 in the rooms where ventilation is actually in
question — meeting rooms and dense open-plan — and leave it out of corridors and server
rooms. The twin reports which rooms have a live NDIR and which are being modelled, so a
partial rollout stays honest rather than becoming invisible.

---

## What each part unlocks in the software

Buying is only worth it if the twin does something with it:

| Part | Unlocks |
|---|---|
| SHT30 | Real zone temperature pins the physics (`tempReal:true`), the AFDD residual becomes meaningful, and the room's **thermal time constant and cooling authority** can be identified (`simulation/dynamics.go`) |
| LD2410C / Rd-03 | Occupancy that survives people sitting still — which is what the setback, the lighting and the learned per-occupant heat gain all depend on |
| ACD1200 | The CO₂ mass balance, and with it each room's **measured air-change rate** and the "ventilation will not keep up with occupancy" prediction |
| IR LED + driver | The control loop actually closing. Without it the twin computes setpoints that reach nothing (`acReal:false` in telemetry says so) |
| SCT-013 | Measured plug load, the APLC after-hours sweep, and the avoided-energy counter |
| Relay ×2 | Lighting and socket actuation — the two things the optimizer can physically change |
