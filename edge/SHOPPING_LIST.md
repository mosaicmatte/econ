# ECON Edge Hardware — Shopping List

What to actually buy to put ECON on real hardware, organised by how far you want to go.
Every part here is referenced by the firmware in `edge/esp32/src/main.cpp`; nothing is
aspirational. Pin assignments and circuits are in [WIRING.md](WIRING.md).

> **Every ✓ price below was re-checked against hshop.vn on 22 Jul 2026**, product page by
> product page, reading each listing's own stock field — not the search results, which keep
> showing a price for items that cannot be bought. SKUs are quoted so you can confirm you
> are holding the same part. Figures without a ✓ are estimates for things hshop does not
> sell at all; treat those as planning numbers, not quotes.

### What moved since the 21 Jul pass

| | |
|---|---|
| **Nothing you already bought changed price.** | All ten linked parts are still in stock at the same figure — SHT30 135k, Rd-03 70k, ACD1200 243k, relay 35k, SCT-013 102k, shifter 10k, PIR 27k, breadboard 35k, jumpers 30k, capacitor kit 40k. |
| **The LD2410 radars are listed again but still cannot be bought.** | LD2410B 98.000₫, LD2410C / LD2410S 105.000₫ — all three pages report out of stock, six days on. The Rd-03 remains both cheaper and available. |
| **PZEM-004T is still out of stock** (225.000₫). | The reason it is not in this list is unchanged. |
| **The gateway estimate was wrong, and in an odd direction.** | A Raspberry Pi 4 4 GB is **3.348.000₫** — *more* than a **Pi 5 2 GB at 2.430.000₫**. The old "1.500–2.500k for a Pi 4" line was well under the real price. Buy the Pi 5. |
| **The microSD estimate was wrong.** | No 32 GB card is stocked. The cheapest usable card is a 64 GB SanDisk at **351.000₫**, not the 120–200k assumed. |
| **The ESP32 line now names a SKU.** | It was a bare ~150k estimate. hshop's stocked boards are 125.000₫ and 190.000₫ — see item 1. |

---

## Pick a build

| Build | What it demonstrates | Parts | ≈ VND / node |
|---|---|---|---|
| **A. Bare demo** | The full software loop with zero wiring — capacitive touch presence on GPIO32, simulated temperature (honestly flagged `tempReal:false`) | ESP32 + cable | ~220k |
| **B. Standard office node** | Real temperature, real presence including people sitting still, real AC control | A + SHT30 + Rd-03 + IR emitter + SSR + breadboard/jumpers | ~615k |
| **C. Fully instrumented** | Everything in B plus measured CO₂ and ventilation identification | B + ACD1200 + level shifter | ~870k |
| **D. Plug-load node (APLC)** | Adds the load a conventional BMS neither meters nor controls | C + SCT-013 clamp + analog front end (2nd SSR channel already on the board) | ~1.01M |

Build **B** is the recommended starting point for a real pilot: it is the cheapest
configuration where every number on the dashboard is measured and every command reaches a
machine. Build **D** is what the plug-load case study (26.4% of office energy) needs.

---

## 1. Core — every node

| # | Part | Qty | Why | ≈ VND |
|---|---|---|---|---|
| 1 | [**ESP32 NodeMCU-32S**, CH340, Ai-Thinker](https://hshop.vn/kit-rf-thu-phat-wifi-ble-esp32-nodemcu-32s-ch340-ai-thinker) `HS1524` | 1 | The node. ESP32-WROOM-32, WiFi, enough GPIO, two ADCs. Every pin in WIRING.md is a GPIO **number**, so the 38-pin NodeMCU and the 30-pin DevKit v1 are interchangeable — only the physical position of the pin differs | ✓ **190.000₫** |
| 1b | *(cheaper alt)* [Mtiny ESP32 WROOM-32E](https://hshop.vn/mach-mtiny-esp32-wroom-32e-arduino-compatible) `HS1760` | 1 | Same WROOM-32E module, 65k less. Fine for a pilot; check its pin header matches your breadboard span before buying four | ✓ **125.000₫** |
| 2 | [Micro-USB cable, 1 m Ugreen](https://hshop.vn/cap-micro-usb-to-usb-2-0-dai-1m-cao-cap-60136-chinh-hang-ugreen) `HS2196V` | 1 | Flashing + power. Must be a **data** cable, not charge-only | ✓ **54.000₫** |
| 2b | *(cheaper alt)* [Micro-USB 30 cm](https://hshop.vn/cap-micro-usb-to-usb-a-2-0-cable-30cm) `HS2188V` | 1 | Half the price, but 30 cm does not reach from a wall socket to a ceiling-height node | ✓ **27.000₫** |
| 3 | [5 V / 2 A USB power supply](https://hshop.vn/nguon-power-adaptor-ac-dc-5v-2a) | 1 | Relays + radar draw more than a laptop port likes | ✓ **45.000₫** |
| 4 | [Breadboard, 830 point](https://hshop.vn/test-board-cammb-102) `HS1113C` | 1 | Power rails down both sides — how the 3.3 V / 5 V split is meant to be laid out | ✓ **35.000₫** |
| 5 | [Jumper wires, M–M ×40 ribbon](https://hshop.vn/day-cam-breadboard-duc-duc-20cm-cap-det-40-soi-m-m-jumper-wire) `HS0381C` | 1 | Splits into single strands, so you can colour-code by rail and match the schematic | ✓ **30.000₫** |

> The ESP32's 3.3 V regulator is good for roughly 500 mA. That covers the board, the
> sensors and the *coils* of two opto-isolated relay boards — but feed relay boards and the
> radar from **5 V (VIN)**, not 3V3. See the power budget in [WIRING.md](WIRING.md).

## 2. Sensing

| # | Part | Qty | Firmware flag | Notes | ≈ VND |
|---|---|---|---|---|---|
| 6 | [**SHT30-IIC** temp + humidity](https://hshop.vn/cam-bien-do-am-nhiet-do-sht30-sht30-iic-temperature-humidity-sensor) (0x44) `HS2214V` | 1 | `-DUSE_SHT30=1` | ±0.2 °C. **Buy this, not a DHT11** — ±2 °C is meaningless against a 2 °C deadband, and it is also what makes a 300-second temperature *slope* readable at all (see the fitness table below) | ✓ **135.000₫** |
| 7 | [**Ai-Thinker Rd-03** 24 GHz mmWave presence](https://hshop.vn/cam-bien-hien-dien-mmwave-24ghz-human-presence-sensing-rd-03-ai-thinker) `HS1779` | 1 | `-DUSE_MMWAVE=1` | Detects a **stationary** person — the single biggest accuracy upgrade for an office, and the fix for the PIR problem below. 3.3 V supply, **OT2 (pin 5) → GPIO18**, no level shifter | ✓ **70.000₫** |
| 7b | *(alt, unbuyable)* HLK-LD2410B / C / S | — | same | Back on the site with prices (98k / 105k / 105k) but **all three still out of stock on 22 Jul**, a week after they went. Same "output high when sensing" contract, so the firmware treats them identically if you source one elsewhere. The Rd-03 is cheaper anyway | — |
| 8 | [**ASAIR ACD1200** NDIR CO₂](https://hshop.vn/cam-bien-khi-co2-acd1200-ndir-carbon-dioxide-sensor-chinh-hang-asair) (0x2A) `HS2167V` | 1 | `-DUSE_CO2=1` | True NDIR, 400–5000 ppm, ±(50 ppm + 5%). What makes demand-controlled ventilation defensible to a tenant, and the **only** input the engine will accept for the air-change model — `roomConditions()` refuses to train the CO₂ balance on a modelled value | ✓ **243.000₫** |
| 9 | [**I²C level shifter** (BSS138)](https://hshop.vn/mach-chuyen-muc-ton-hieu-i2c) `HS0667` | 1–2 | with #8 | **Mandatory, not optional.** Two BSS138 MOSFETs with 10 kΩ pull-ups — the correct I²C topology. At 10.000₫, buy a spare | ✓ **10.000₫** |
| 10 | [*(optional)* PIR HC-SR501](https://hshop.vn/cam-bien-chuyen-dong-pir-5v-2) `HS0158C` | 1 | `-DUSE_PIR=1` | 5 V supply, 3.3 V logic out, no shifter. Cheap motion presence; OR-ed with the radar if both fitted | ✓ **27.000₫** |

## 3. Actuation

| # | Part | Qty | Firmware flag | Notes | ≈ VND |
|---|---|---|---|---|---|
| 11 | [**2-channel SSR, 5 V (G3MB-202P)**](https://hshop.vn/module-2-relay-ran-ssr-5vdc) `HS0996` | 1 | default + `-DUSE_PLUG=1` | **In stock — buy this.** One board, both actuators: `CH1 → GPIO23` lights, `CH2 → GPIO25` socket. Trigger input is TTL 3.3–5 V (drives straight from the ESP32), high-level trigger (matches the firmware), DC+ on 5 V. **AC-only, 0.1–2 A** — a lamp needs ≥ ~25 W to clear the SSR's minimum, and a real socket circuit needs a contactor. Full wiring + specs in [WIRING §4 Option B](WIRING.md). | ✓ **59.000₫** |
| 11b | *(mechanical, out of stock)* [2-ch opto relay High/Low](https://hshop.vn/module-2-relay-voi-opto-coch-ly-koch-h-l-5vdc) `HS0998C` | 1 | same | Dry contacts (COM/NO/NC), 10 A, switches AC **or** DC. Cheaper and no minimum-load quirk — buy it instead **when the 5 VDC variant restocks** (both it and `HS0997` were *Hết hàng* on 23 Jul 2026). Jumper to HIGH, coils on 5 V. | ✓ 35.000₫ |
| 12 | **IR LED 940 nm, 5 mm** | 1–2 | `-DUSE_IR_AC=1` | Drives the split unit. Two in series widens coverage in a large room | 5–10k |
| 13 | **NPN transistor** 2N2222 / S8050 / PN2222 | 1 | with #12 | **Required.** A GPIO sources ~12 mA; an IR LED wants ~100 mA of pulse current for useful range. Straight off the pin you get about a metre | 3–5k |
| 14 | Resistors: **1 kΩ** (transistor base), **10 Ω** ¼ W (LED current) | 1 ea | with #12 | See the driver circuit in WIRING.md | ~1k ea |

> ⚠️ **hshop does not stock items 12–14, and this pass confirmed it.** Searches for a 940 nm
> emitter, a discrete NPN and a ¼ W resistor assortment all come back with nothing usable —
> the hits are reflective *sensor* modules, not emitters. Buy these three at a general
> component shop (Nhật Tảo) or online. They are the cheapest items on the whole list and
> the only ones that will cost you a second trip, so put them in the same order.

> **Which IR protocol?** Build with `-DUSE_IR_AC=1 -DIR_AC_PROTOCOL=COOLIX` first — it
> covers many budget/OEM splits. For Daikin / Panasonic / Mitsubishi / LG / Samsung /
> Toshiba / Gree (Casper and Nagakawa are often Gree-compatible) set the matching protocol;
> all eight are verified to compile. If the unit ignores you, capture its handset with
> IRremoteESP8266's `IRrecvDumpV3` example and use whatever it decodes to.

## 4. Plug-load metering (build D)

| # | Part | Qty | Notes | ≈ VND |
|---|---|---|---|---|
| 15 | [**SCT-013** split-core CT, 100 A](https://hshop.vn/cam-bien-dong-dien-hall-100a-yhdc) `HS0186` | 1 | **The credibility item.** Clips over insulation — no mains contact, no electrician, no permit. Reads true-RMS on GPIO34 and publishes `plugW`, replacing the modelled plug draw. Current-output, so it needs the burden below | ✓ **102.000₫** |
| 15b | *(alt)* **SCT-013-030** (30 A : 1 V) | 1 | Voltage-output — **skip the burden** and build `-DPLUG_CAL_A_PER_V=30.0` | — |
| 16 | [Ceramic capacitor assortment](https://hshop.vn/bo-23-loai-tu-gom-thong-dung-6pf-0-1uf-23-kind-ceramic-capacitor) `HS0093` | 1 | A **0.1 µF** from this kit ties the bias node to ground. (OpenEnergyMonitor specifies 10 µF; 0.1 µF is fine at the ESP32's sampling rate, and the kit is what hshop actually stocks. The listing now reads "22 loại" where it used to read 23 — same kit, same range, same price) | ✓ **40.000₫** |
| 17 | **33 Ω** burden + 2× **10 kΩ** bias resistors, ¼ W | 1 set | The burden sets the scale (`-DPLUG_CAL_A_PER_V=60.6`); the 10 kΩ pair forms the 1.65 V mid-rail. Not stocked at hshop — same trip as items 12–14 | ~5k |

> **No 3.5 mm jack needed.** Snip the SCT-013's plug and land its two bare leads directly on
> the breadboard. One less part to source.

> ⚠️ **The clamp goes around ONE conductor — the live wire only.** Clamped around a whole
> two-core cord it reads ~zero, because the live and neutral currents cancel. This means
> opening the circuit's enclosure. **In Vietnam, have a licensed electrician do the mains
> side.** The CT itself is non-contact and safe; getting to the single conductor is not.

## 5. Gateway (one per site, not per node)

| # | Part | Qty | Why | ≈ VND |
|---|---|---|---|---|
| 21 | [**Raspberry Pi 5, 2 GB**](https://hshop.vn/may-tinh-raspberry-pi-5-made-in-uk) `HS2256V` | 1 | Hosts the Mosquitto broker and `gateway.py`, the failsafe rules engine that keeps a vacant zone from being left lit and cooled when the Go engine is unreachable. 2 GB is ample for that job — 4 GB is 3.726k, 8 GB 5.724k, 16 GB 10.044k, and none of it buys anything the gateway uses | ✓ **2.430.000₫** |
| 21b | *(budget)* [Raspberry Pi Zero 2 W](https://hshop.vn/may-tinh-raspberry-pi-zero-2-w) | 1 | A broker and a rules loop are not demanding. Saves 1.7M and drops the PSU to the same 5 V 2 A brick the nodes use | ✓ **729.000₫** |
| 21c | *(do not buy)* Raspberry Pi 4 Model B 4 GB | — | **3.348.000₫** — nearly a million dong *more* than a Pi 5 and a generation behind. The Pi 3B+ is out of stock | ✓ 3.348.000₫ |
| 22 | [microSD 64 GB SanDisk (A1, Class 10)](https://hshop.vn/the-nho-sandisk-microsdxc-class-10-uhs-i-100mb-s-64gb) `HS1122V` | 1 | No 32 GB card is stocked; the 8 GB low-speed one is out of stock and too slow anyway | ✓ **351.000₫** |
| 23 | [Official Pi PSU 5.1 V 3 A USB-C](https://hshop.vn/nguon-chinh-hang-official-raspberry-pi-power-supply-5-1vdc-3a-usb-c) | 1 | Under-powering a Pi causes exactly the kind of intermittent fault you will waste a day on | ✓ **297.000₫** |
| 23b | [Waveshare 27 W USB-C PD](https://hshop.vn/waveshare-27w-usb-type-c-power-supply-type-c-pd-power-supply-suitable-for-raspberry-pi-5) `HS2377V` | 1 | Cheaper *and* the correct supply for a Pi 5, which asks for 5 A/27 W before it will grant the full USB peripheral budget. Pick this one if you bought item 21 | ✓ **243.000₫** |

Any always-on Linux box works for a pilot; the Pi is the deployable form factor.

## 6. Optional second node type

| # | Part | Qty | Why | ≈ VND |
|---|---|---|---|---|
| 24 | [**Raspberry Pi Pico W**](https://hshop.vn/mach-vi-dieu-khien-raspberry-pi-pico-w-rp2040-wifi-bluetooth) | 1 | The `edge/pico` firmware: RP2040 internal temperature, BOOTSEL/GP16 presence, onboard LED as the lighting actuator, 8 s watchdog. The **W** joins MQTT directly; a plain Pico speaks JSON over USB via `bridge.py` — but hshop no longer lists a non-W RP2040 Pico, and the Pico 2 is out of stock, so the W is the one to buy | ✓ **195.000₫** |
| 24b | *(alt)* [Raspberry Pi Pico 2 W (RP2350)](https://hshop.vn/mach-raspberry-pi-pico-2-w-rp2350) | 1 | Newer silicon, 80k more. The firmware does not use anything the RP2350 adds | ✓ **275.000₫** |

## 7. From bench to building — what an installed system adds

Everything above builds a node that works on a desk. An installed one differs in a way no
parts list survives unchanged: **it is not one box.** In a real room the sensor belongs on a
wall in the breathing zone, the IR emitter needs line of sight to the indoor unit, the
lighting switch belongs in the luminaire's junction box and the CT belongs at the
distribution board. Those are metres to tens of metres apart, and **I²C does not span
that** — it is a board-level bus with no differential pair, no shielding and no recovery
beyond a NAK. Extend it down a 4 m cable and you get plausible numbers that are wrong.

> **The engine already handles the split.** `IngestTelemetry` guards every field behind a
> nil check and timestamps each one separately, so two boards publishing to the *same* zone —
> one sending only temperature and CO₂, the other only `acReal` and `plugW` — **merge rather
> than overwrite**. A wall sensor head plus a plant-side controller needs no engine change.
> That is what the ESP32 and the Pico are for: controller and sensor head. It only works if
> the Pico is a **W** — a plain Pico depends on `bridge.py` tailing a USB cable, which is a
> bench arrangement, not an installation.

| # | Part | Why an install needs it | ≈ VND |
|---|---|---|---|
| 30 | [**W5500 Ethernet SPI**](https://hshop.vn/mach-chuyen-giao-tiep-ethernet-spi-wiznet-w5500) `HS0654V` | Wired networking for the controller. Building WiFi is usually a guest SSID with NAT and rate limits; a controller that must accept setpoint commands should not depend on it. Also makes OTA updates reliable | ✓ **105.000₫** |
| 31 | [**MKE-M20 RS-485/TTL** with GDT](https://hshop.vn/mach-chuyen-giao-tiep-mke-m20-rs485-ttl-gdt-module) `HS2253V` | The answer when a sensor head cannot use WiFi: a shielded pair carries Modbus a kilometre where I²C dies at 30 cm. The **GDT** is a gas discharge tube — surge protection on a long field run, which is what separates an industrial transceiver from a bare MAX485 | ✓ **125.000₫** |
| 32 | [**2-ch solid state relay**, 2 A/240 V](https://hshop.vn/module-2-relay-ran-ssr-5vdc) `HS0996` | No contacts to weld or wear, silent, and no coil inrush. A mechanical relay switching a lighting circuit several times a day is a consumable; an SSR is not. Watch the 2 A ceiling — [4-ch](https://hshop.vn/module-4-relay-ran-ssr-5vdc) is 115.000₫, and a real luminaire bank needs a contactor the SSR pilots | ✓ **59.000₫** |
| 33 | [DIN-rail ABS case (Pi 5)](https://hshop.vn/vo-bao-ve-bang-nhua-abs-din-rail-case-for-raspberry-pi-5) `HS2110V` | Gets the gateway onto a rail in a cabinet instead of sitting on a shelf | ✓ **135.000₫** |
| 34 | [*(alt)* Waveshare 8-ch Ethernet/PoE Modbus relay](https://hshop.vn/bo-dieu-khien-waveshare-8-ch-modbus-poe-ethernet-relay-multi-protection) `HS1814V` | If you would rather not build the actuation side at all: eight channels, Modbus TCP/RTU, PoE powered, already industrial. One of these replaces the relay board, the enclosure and the node's power supply for a floor's lighting | ✓ **1.188.000₫** |

**Wanted but out of stock on 22 Jul:** the [ADS1115 16-bit I²C ADC](https://hshop.vn/mach-chuyen-tin-hieu-adc-ads1115-16-bit-4-channel-i2c) (75.000₫) — worth revisiting, because the ESP32's own ADC is noticeably nonlinear and it is the weakest link in the CT measurement; a [DS3231 RTC](https://hshop.vn/mach-thoi-gian-thuc-rtc-ds3231) (44.000₫) for timestamps that survive an NTP outage; and both PC817 opto isolation boards.

**hshop cannot supply the rest of an install at all**, and this is not an oversight — it is an
electronics shop, not an electrical wholesaler. A DIN-rail SMPS (Mean Well HDR/DR series),
an IP54 enclosure, DIN terminal blocks, fuse holders, cable glands and shielded twisted pair
all come from an electrical supplier. Budget them separately.

### The firmware gap is the larger half

Parts are the easy part. Before this goes into a building the ESP32 firmware needs:

- **OTA update.** You cannot USB-flash forty ceiling boxes. This is the single hardest
  blocker, because without it every later fix requires a ladder.
- **MQTT over TLS with per-node credentials.** Today it is anonymous on port 1883 — fine on
  a bench, not on a tenant's network.
- **Identity and calibration in NVS**, not `wifi_secrets.h` and `-D` build flags baked per
  board. Every node currently needs its own build; that does not scale past a handful, and
  per-unit CT calibration constants have nowhere to live.
- **A local fail-safe policy** for what the relays do when the broker has been unreachable
  longer than the watchdog. `gateway.py` covers the engine being down; it does not cover the
  node being cut off from the gateway.

The Pico firmware already has its 8 s watchdog. The ESP32 side has none of the above.

---

## Worked bundle — a 4-zone pilot

One instrumented floor: three standard office nodes plus one plug-load node, on one gateway.

| Item | Qty | ≈ VND |
|---|---|---|
| ESP32 NodeMCU-32S @190k | 4 | 760k |
| Micro-USB 1 m + 5 V 2 A PSU @99k | 4 | 396k |
| SHT30-IIC @135k | 4 | 540k |
| Rd-03 mmWave radar @70k | 4 | 280k |
| 2-channel SSR @59k *(mechanical relay out of stock)* | 4 | 236k |
| IR LED + transistor + resistors (off-site) | 4 | ~60k |
| Breadboard + jumper ribbon @65k | 4 | 260k |
| ACD1200 + level shifter @253k | 2 | 506k |
| SCT-013 + capacitor kit + passives | 1 | ~147k |
| Pi 5 2 GB + 64 GB SD + 27 W PSU | 1 | 3.024k |
| **Total** | | **≈ 6.21M VND** |

**Value variant, ≈ 3.95M.** Swap the node board for the Mtiny ESP32 (125k), the cable for
the 30 cm one (27k), and the gateway for a Pi Zero 2 W + 64 GB SD + 5 V 2 A brick (1.125k).
Nothing measured changes — every sensor, actuator and firmware flag is identical, and the
twin cannot tell the difference. The saving is entirely in the two parts that only host
software.

The actuator line rose 140k → 236k because the 35k mechanical relay is out of stock and the
in-stock swap is the 59k SSR (§4 Option B). When the mechanical board restocks it drops back
to 140k, taking both totals down ~96k (≈ 6.11M / ≈ 3.85M). Every other line was priced on
hshop on 22–23 Jul and is unchanged.

CO₂ is still the expensive sensing line. Fit the ACD1200 in the rooms where ventilation is
actually in question — meeting rooms and dense open-plan — and leave it out of corridors
and server rooms. The twin reports which rooms have a live NDIR and which are being
modelled, so a partial rollout stays honest rather than becoming invisible.

---

## Does this hardware still fit the software?

The twin no longer just scores rooms against learned baselines — `simulation/dynamics.go`
now *identifies* each room, fitting two first-order balances by recursive least squares:

```
thermal:  dT/dt = θ₀·(T_out − T_in) + θ₁·flow·(T_in − T_supply) + θ₂·occupancy + θ₃
co2:      dC/dt = φ₀·occupancy + φ₁·(C_out − C) + φ₂
```

Those coefficients are physical — 1/θ₀ is the room's thermal time constant, θ₁ its cooling
authority, φ₁ its measured air-change rate — and they are what the learned setback ceiling,
the ETA predictions and the "ventilation will not keep up" warnings are computed from. So
the honest question for a parts list is no longer "does the firmware read this sensor" but
**"is every term in those two equations measured?"**

| Term | Where it comes from today | Verdict |
|---|---|---|
| `T_in` | **SHT30**, ±0.2 °C, published every 5 s | **Fits.** The regressor works on a temperature *slope* over a 300 s window; a ±2 °C DHT11 would put more noise in the slope than there is signal. This is the part of the list that justifies its price twice over |
| `C` | **ACD1200** NDIR. `roomConditions()` sets `Co2Live` only for a fresh hardware reading, so a room without one is never used to teach the CO₂ balance | **Fits, and is enforced in code** |
| `occupancy` | **Rd-03** — but it reports *presence*, 0 or 1 | **Partly.** θ₂ is °C/hr **per occupant** and φ₀ is ppm/hr **per occupant**. Fed a 0/1, both are identified per *occupied room* instead, and a meeting room with twelve people looks like a broom cupboard with one. The head count exists — `ai_modules/branch_a_occupancy` runs YOLO + ByteTrack and publishes an integer on the same MQTT contract — it simply is not on this list, because a laptop webcam covers it for a pilot and hshop stocks no USB camera |
| `T_out` | The weather poller, one reading for the whole building | **Acceptable.** One sky over 891 zones is a fair approximation, and `outdoorStaleAfter` stops a dead poller from driving the envelope forever |
| `flow` | `v.Flow / v.NominalFlow` — the **simulated** VAV serving that zone (`engine.go:917`) | **Does not fit.** No part on this list measures it, and a real room on a split AC has no VAV at all |
| `T_supply` | `supplyAirC = 12.0`, a **constant** (`dynamics.go:96`) | **Does not fit.** A split unit's discharge is nowhere near 12 °C, and it moves with compressor state |

**The bottom line:** everything the list is *already* spent on is correctly chosen — the
SHT30's precision, the Rd-03's stationary-person detection and the ACD1200's true NDIR are
each load-bearing for a specific coefficient. But θ₁, the cooling-authority term that
`SetbackCeiling()` and the capability-shortfall recommendations both lean on, is being fit
against two inputs that no sensor in this BOM observes. For a simulated zone that is fine —
the twin knows its own VAV. For the physical rooms this list is meant to instrument, it
means the most operationally consequential coefficient is regressed partly on the twin's own
assumption.

### Three parts that close it — now in the firmware

All three are stocked, cheap, and land on pins held free for them. **Each replaces a value
the twin currently assumes with one it measures**, and each is behind its own build flag so
a board fitted with one still reports honestly about the others. Buy them when convenient;
the software is already waiting.

| Part | SKU | ≈ VND | Flag | Turns this assumption into a measurement |
|---|---|---|---|---|
| [DS18B20 waterproof probe](https://hshop.vn/cam-bien-nhiet-do-chong-nuoc-mke-s15-ds18b20-waterproof-temperature-sensor) | `HS2243V` | ✓ **75.000₫** | `-DUSE_SUPPLY_TEMP=1` → **GPIO26** | `T_supply`, today a 12 °C constant. Cable-tie it in the indoor unit's discharge louvre and the cooling regressor is referenced to the real discharge. Its gap to room temperature is also a better "is the compressor running" signal than `acReal`, which only says the firmware *sent* an IR frame |
| A second [SCT-013](https://hshop.vn/cam-bien-dong-dien-hall-100a-yhdc) on the AC supply | `HS0186` | ✓ **102.000₫** | `-DUSE_AC_CLAMP=1` → **GPIO35** | `flow`, today the twin's own simulated VAV. The compressor's power draw *is* the cooling drive term. ADC1, input-only, same front end as the plug clamp |
| [BH1750 ambient light](https://hshop.vn/cam-bien-cuong-do-onh-song-lux-bh1750) | `HS0159V` | ✓ **35.000₫** | `-DUSE_LUX=1` → I²C `0x23` | Solar gain, today a static per-zone multiplier with no time-of-day or cloud response. A lux reading is a real irradiance proxy and the precondition for daylight-linked dimming. Shares the existing I²C bus, 3.3 V, no shifter |

**212.000₫ for all three.** The node publishes `supplyC`, `acW` and `lux`; the engine
ingests all three and already uses `supplyC` in place of the design constant wherever a
probe reports. Fields are **omitted, never defaulted**, when a sensor is absent or fails —
a fabricated zero on the AC clamp would tell the twin the compressor is off.

### Also worth a look, not yet in firmware

| Part | ≈ VND | Why it might earn its place |
|---|---|---|
| [ASAIR APM2000 laser PM1.0/2.5/10](https://hshop.vn/cam-bien-bui-min-apm2000-laser-particulate-matter-pm1-0-pm2-5-pm10-sensor-asair) `HS2169V` | ✓ 270.000₫ | Indoor air quality is a *scored* comfort criterion, and CO₂ alone does not cover particulates — the thing occupants in a Vietnamese city actually notice |
| [ACS712 30 A Hall current sensor](https://hshop.vn/cam-bien-dong-dien-hall-acs712-30a) `HS0187C` | ✓ 27.000₫ | A quarter the price of an SCT-013, but it goes *inline* — it breaks the circuit, so it is bench/appliance metering, not a retrofit clamp |
| [AC current transformer, 5 A](https://hshop.vn/cam-bien-dong-dien-ac-current-transformer-sensor-5a) `HS1275` | ✓ 18.000₫ | For a small single split unit the 100 A clamp is wildly oversized; a 5 A CT gives far better resolution on the same front end |

### The original two-part note

### Two parts that would close it

Both are stocked, both are cheap, and both land on ESP32 pins that are currently free.
**Neither is in the firmware yet** — they are listed here as the next build, not as
something to buy today, and they are deliberately kept out of the bundle totals above.

| Part | ≈ VND | What it turns from assumed into measured |
|---|---|---|
| [DS18B20 waterproof probe (MKE-S15)](https://hshop.vn/cam-bien-nhiet-do-chong-nuoc-mke-s15-ds18b20-waterproof-temperature-sensor) `HS2243V` | ✓ **75.000₫** | Cable-tied inside the indoor unit's discharge louvre it measures `T_supply` directly, replacing the 12.0 constant. Its gap to room temperature is also a far better answer to "is the compressor actually running" than `acReal`, which today only reports that the firmware *sent* an IR frame. 1-Wire, one free GPIO, no shifter |
| A second [SCT-013](https://hshop.vn/cam-bien-dong-dien-hall-100a-yhdc) `HS0186` on the AC's own supply | ✓ **102.000₫** | The split unit's compressor power **is** the cooling drive term. Clamping it makes `flow` a measured regressor instead of a simulation artifact. GPIO35 is free on ADC1, and the analog front end is the one already documented for the plug clamp |

A third, cheaper option needs no purchase at all: point `ai_modules/branch_a_occupancy` at a
webcam in the same room and let it publish the head count. **Do not run it and a radar on
the same zone** — `IngestTelemetry` takes whichever occupancy arrives last, so a radar's 0/1
will overwrite a camera's count every 5 seconds.

---

## Deliberately **not** on this list

- **MQ-135** — sold as an "air quality" sensor and widely mistaken for a CO₂ sensor. It
  cannot measure CO₂. It is a broadband tin-oxide sensor whose "CO₂" output is extrapolated
  from a curve it never measures. Fitting one would put a fabricated number on the
  ventilation model, which is precisely what the rest of this system is built to avoid.
- **DHT11 / DHT22** — ±2 °C against a 2 °C control deadband, and hshop does not stock the
  DHT22 anyway. The firmware still supports DHT as a fallback (`-DUSE_DHT=1
  -DDHT_TYPE=DHT11`), but do not plan around it.
- **Sensirion SCD4x** — the obvious CO₂ upgrade, and hshop simply does not carry it.
- **DFRobot 0–50000 ppm CO₂ (2.241.000₫, in stock)** — nine times the ACD1200 to measure a
  range no office ever reaches. Indoor air lives between 400 and 2000 ppm.
- **PZEM-004T energy meter (225.000₫)** — true V/A/W/kWh/PF over UART, and genuinely better
  metering than a clamp. Three reasons it is not here: it is **still out of stock** as of
  22 Jul, **its voltage sense leads land on live 220 V** — licensed-electrician work, and in
  a commercial building a landlord-permission and liability question — and there is no
  driver for it in the firmware. **The SCT-013 gets you metered watts this month with no
  mains contact** — buy that instead.

---

## What each part unlocks in the software

Buying is only worth it if the twin does something with it:

| Part | Unlocks |
|---|---|
| SHT30 | Real zone temperature pins the physics (`tempReal:true`), the AFDD residual becomes meaningful, and the room's **thermal time constant and cooling authority** can be identified (`simulation/dynamics.go`) |
| Rd-03 (or an LD2410, if you find one) | Occupancy that survives people sitting still — which is what the setback, the lighting and the learned per-occupant heat gain all depend on |
| ACD1200 | The CO₂ mass balance, and with it each room's **measured air-change rate** and the "ventilation will not keep up with occupancy" prediction |
| IR LED + driver | The control loop actually closing. Without it the twin computes setpoints that reach nothing (`acReal:false` in telemetry says so) |
| SCT-013 | Measured plug load, the APLC after-hours sweep, and the avoided-energy counter |
| Relay ×2 | Lighting and socket actuation — the two things the optimizer can physically change |
