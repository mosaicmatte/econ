# ECON Edge Hardware — Shopping List

What to actually buy to put ECON on real hardware, organised by how far you want to go.
Every part here is referenced by the firmware in `edge/esp32/src/main.cpp`; nothing is
aspirational. Pin assignments and circuits are in [WIRING.md](WIRING.md).

> **Prices marked ✓ were checked against hshop.vn on 21 Jul 2026 and were in stock.**
> Unmarked figures are still estimates — treat them as planning numbers, not quotes.
> Availability moves fast: the 85.000₫ SHT30 module and the **entire HLK-LD2410 radar
> family** both went out of stock on 16 Jul, which is why the parts below are what they are.

> **Sourcing reality check.** DHT22, MH-Z19 and Sensirion SCD4x are generally *not* stocked
> in Vietnam. The parts below are chosen because they are: SHT30 instead of DHT22, ASAIR
> ACD1200 instead of MH-Z19, Ai-Thinker Rd-03 instead of the HLK-LD2410C.

---

## Pick a build

| Build | What it demonstrates | Parts | ≈ VND / node |
|---|---|---|---|
| **A. Bare demo** | The full software loop with zero wiring — capacitive touch presence on GPIO32, simulated temperature (honestly flagged `tempReal:false`) | ESP32 + cable | ~180k |
| **B. Standard office node** | Real temperature, real presence including people sitting still, real AC control | A + SHT30 + Rd-03 + IR emitter + relay board + breadboard/jumpers | ~500k |
| **C. Fully instrumented** | Everything in B plus measured CO₂ and ventilation identification | B + ACD1200 + level shifter | ~755k |
| **D. Plug-load node (APLC)** | Adds the load a conventional BMS neither meters nor controls | C + SCT-013 clamp + analog front end (2nd relay channel already on the board) | ~900k |

Build **B** is the recommended starting point for a real pilot: it is the cheapest
configuration where every number on the dashboard is measured and every command reaches a
machine. Build **D** is what the plug-load case study (26.4% of office energy) needs.

---

## 1. Core — every node

| # | Part | Qty | Why | ≈ VND |
|---|---|---|---|---|
| 1 | **ESP32 DevKit v1** (30-pin, CP2102 or CH340) | 1 | The node. WiFi + enough GPIO + two ADCs. ESP32-WROOM-32 NodeMCU is the same pinout | ~150k |
| 2 | Micro-USB cable (**data**, not charge-only) | 1 | Flashing + power | 20–40k |
| 3 | 5 V / 2 A USB power supply | 1 | Relays + radar draw more than a laptop port likes | 50–80k |
| 4 | [Breadboard, 830 point](https://hshop.vn/test-board-cammb-102) | 1 | Power rails down both sides — how the 3.3 V / 5 V split is meant to be laid out | ✓ **35.000₫** |
| 5 | [Jumper wires, M–M ×40 ribbon](https://hshop.vn/day-cam-breadboard-duc-duc-20cm-cap-det-40-soi-m-m-jumper-wire) | 1 | Splits into single strands, so you can colour-code by rail and match the schematic | ✓ **30.000₫** |

> The ESP32's 3.3 V regulator is good for roughly 500 mA. That covers the board, the
> sensors and the *coils* of two opto-isolated relay boards — but feed relay boards and the
> radar from **5 V (VIN)**, not 3V3. See the power budget in [WIRING.md](WIRING.md).

## 2. Sensing

| # | Part | Qty | Firmware flag | Notes | ≈ VND |
|---|---|---|---|---|---|
| 6 | [**SHT30-IIC** temp + humidity](https://hshop.vn/cam-bien-do-am-nhiet-do-sht30-sht30-iic-temperature-humidity-sensor) (0x44) | 1 | `-DUSE_SHT30=1` | ±0.2 °C. **Buy this, not a DHT11** — ±2 °C is meaningless against a 2 °C deadband, and the AFDD residual would be measuring the sensor. *(The 85.000₫ breakout sold out 16 Jul; this is the same SHT30 chip on a different board — same address, same protocol, firmware unchanged.)* | ✓ **135.000₫** |
| 7 | [**Ai-Thinker Rd-03** 24 GHz mmWave presence](https://hshop.vn/cam-bien-hien-dien-mmwave-24ghz-human-presence-sensing-rd-03-ai-thinker) | 1 | `-DUSE_MMWAVE=1` | Detects a **stationary** person — the single biggest accuracy upgrade for an office, and the fix for the PIR problem below. 3.3 V supply, **OT2 (pin 5) → GPIO18**, no level shifter | ✓ **70.000₫** |
| 7b | *(was the default)* HLK-LD2410C | — | same | **The whole LD2410 family — 2410B/C/S, 2420, 2450 — went out of stock at hshop on 16 Jul.** Same "output high when sensing" contract, so the firmware treats it identically if you source one elsewhere. The Rd-03 is cheaper anyway | — |
| 8 | [**ASAIR ACD1200** NDIR CO₂](https://hshop.vn/cam-bien-khi-co2-acd1200-ndir-carbon-dioxide-sensor-chinh-hang-asair) (0x2A) | 1 | `-DUSE_CO2=1` | True NDIR, 400–5000 ppm, ±(50 ppm + 5%). What makes demand-controlled ventilation defensible to a tenant, and what the air-change identification needs | ✓ **243.000₫** |
| 9 | [**I²C level shifter** (BSS138)](https://hshop.vn/mach-chuyen-muc-ton-hieu-i2c) | 1–2 | with #8 | **Mandatory, not optional.** Two BSS138 MOSFETs with 10 kΩ pull-ups — the correct I²C topology. At 10.000₫, buy a spare | ✓ **10.000₫** |
| 10 | [*(optional)* PIR HC-SR501](https://hshop.vn/cam-bien-chuyen-dong-pir-5v-2) | 1 | `-DUSE_PIR=1` | 5 V supply, 3.3 V logic out, no shifter. Cheap motion presence; OR-ed with the radar if both fitted | ✓ **27.000₫** |

## 3. Actuation

| # | Part | Qty | Firmware flag | Notes | ≈ VND |
|---|---|---|---|---|---|
| 11 | [**2-channel opto relay, 5 V**](https://hshop.vn/module-2-relay-voi-opto-coch-ly-koch-h-l-5vdc) | 1 | default + `-DUSE_PLUG=1` | **One board covers both actuators.** `IN1 → GPIO23` switches the zone lights, `IN2 → GPIO25` switches the non-critical socket circuit for the after-hours sweep. Set the board's jumper to **high-level trigger** to match the firmware, and power the coils from 5 V | ✓ **35.000₫** |
| 12 | **IR LED 940 nm, 5 mm** | 1–2 | `-DUSE_IR_AC=1` | Drives the split unit. Two in series widens coverage in a large room | 5–10k |
| 13 | **NPN transistor** 2N2222 / S8050 / PN2222 | 1 | with #12 | **Required.** A GPIO sources ~12 mA; an IR LED wants ~100 mA of pulse current for useful range. Straight off the pin you get about a metre | 3–5k |
| 14 | Resistors: **1 kΩ** (transistor base), **10 Ω** ¼ W (LED current) | 1 ea | with #12 | See the driver circuit in WIRING.md | ~1k ea |

> The IR items are the one part of this list the 21 Jul sourcing pass predates — real AC
> control (`USE_IR_AC`) landed after it. They are cheap and generic; any 940 nm LED and any
> small-signal NPN will do.

> **Which IR protocol?** Build with `-DUSE_IR_AC=1 -DIR_AC_PROTOCOL=COOLIX` first — it
> covers many budget/OEM splits. For Daikin / Panasonic / Mitsubishi / LG / Samsung /
> Toshiba / Gree (Casper and Nagakawa are often Gree-compatible) set the matching protocol;
> all eight are verified to compile. If the unit ignores you, capture its handset with
> IRremoteESP8266's `IRrecvDumpV3` example and use whatever it decodes to.

## 4. Plug-load metering (build D)

| # | Part | Qty | Notes | ≈ VND |
|---|---|---|---|---|
| 15 | [**SCT-013** split-core CT, 100 A](https://hshop.vn/cam-bien-dong-dien-hall-100a-yhdc) | 1 | **The credibility item.** Clips over insulation — no mains contact, no electrician, no permit. Reads true-RMS on GPIO34 and publishes `plugW`, replacing the modelled plug draw. Current-output, so it needs the burden below | ✓ **102.000₫** |
| 15b | *(alt)* **SCT-013-030** (30 A : 1 V) | 1 | Voltage-output — **skip the burden** and build `-DPLUG_CAL_A_PER_V=30.0` | — |
| 16 | [Ceramic capacitor assortment](https://hshop.vn/bo-23-loai-tu-gom-thong-dung-6pf-0-1uf-23-kind-ceramic-capacitor) | 1 | A **0.1 µF** from this kit ties the bias node to ground. (OpenEnergyMonitor specifies 10 µF; 0.1 µF is fine at the ESP32's sampling rate, and the kit is what hshop actually stocks) | ✓ **40.000₫** |
| 17 | **33 Ω** burden + 2× **10 kΩ** bias resistors, ¼ W | 1 set | The burden sets the scale (`-DPLUG_CAL_A_PER_V=60.6`); the 10 kΩ pair forms the 1.65 V mid-rail. hshop sells modules, not loose resistors — pull these from any starter assortment | ~5k |

> **No 3.5 mm jack needed.** Snip the SCT-013's plug and land its two bare leads directly on
> the breadboard. One less part to source.

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
| ESP32 DevKit v1 + cable + PSU | 4 | ~980k |
| SHT30-IIC | 4 | 540k |
| Rd-03 mmWave radar | 4 | 280k |
| 2-channel opto relay | 4 | 140k |
| IR LED + transistor + resistors | 4 | ~60k |
| Breadboard + jumper ribbon | 4 | 260k |
| ACD1200 + level shifter | 2 | 506k |
| SCT-013 + capacitor kit + passives | 1 | ~147k |
| Raspberry Pi 4 + SD + PSU | 1 | ~2,300k |
| **Total** | | **≈ 5.2M VND** |

That is **~1.5M below** the first draft of this list, almost entirely because the ACD1200
is 243.000₫ rather than the ~650.000₫ estimated before the parts were actually looked up —
and because the Rd-03 replaced a radar that costs more and is out of stock.

CO₂ is the expensive line. Fit the ACD1200 in the rooms where ventilation is actually in
question — meeting rooms and dense open-plan — and leave it out of corridors and server
rooms. The twin reports which rooms have a live NDIR and which are being modelled, so a
partial rollout stays honest rather than becoming invisible.

---

## Deliberately **not** on this list

- **MQ-135** — sold as an "air quality" sensor and widely mistaken for a CO₂ sensor. It
  cannot measure CO₂. It is a broadband tin-oxide sensor whose "CO₂" output is extrapolated
  from a curve it never measures. Fitting one would put a fabricated number on the
  ventilation model, which is precisely what the rest of this system is built to avoid.
- **DHT11 / DHT22** — ±2 °C against a 2 °C control deadband, and hshop does not stock the
  DHT22 anyway. The firmware still supports DHT as a fallback (`-DUSE_DHT=1
  -DDHT_TYPE=DHT11`), but do not plan around it.
- **DFRobot 0–50000 ppm CO₂ (~2.241.000₫)** — nine times the ACD1200 to measure a range no
  office ever reaches. Indoor air lives between 400 and 2000 ppm.
- **PZEM-004T energy meter (~225.000₫)** — true V/A/W/kWh/PF over UART, and genuinely
  better metering than a clamp. Two reasons it is not here: it was **out of stock** at hshop
  on 16 Jul, and **its voltage sense leads land on live 220 V**. That is licensed-electrician
  work and, in a commercial building, a landlord-permission and liability question. There is
  also no driver for it in the firmware yet. **The SCT-013 gets you metered watts this month
  with no mains contact** — buy that instead.

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
