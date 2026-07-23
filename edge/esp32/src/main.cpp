// ECON Edge Node — ESP32 firmware
// -----------------------------------------------------------------------------
// Role in the system (see econ/BACKEND_ARCHITECTURE.md):
//   * Publishes zone sensor telemetry  -> econ/telemetry/<zone>
//   * Subscribes to actuation commands  <- econ/commands/<zone>
//   * Drives a lighting relay + an HVAC IR emitter from those commands.
//
// Command format spoken by the Go engine's optimizer (engine.actuate):
//     "LIGHTS_ON;SETPOINT=22.0"   /   "LIGHTS_OFF;SETPOINT=26.0"
// (legacy "HVAC_SET:<c>" is still accepted). This firmware PARSES that combined
// string — earlier sketches only matched the literal "LIGHTS_ON", which never
// fires against the real payload.
//
// Telemetry payload (what mqtt.go / yolo_tracker.py expect):
//     {"zone":"Level 4","occupancy":N,"temperature":t,"humidity":h,"co2":c}
//
// Sensors are simulated by default so the node runs on a bare ESP32, but presence is
// REAL out of the box: USE_TOUCH_PRESENCE reads the ESP32's capacitive touch pin
// (GPIO32) — pinch a jumper wire on it and the zone shows occupied on the dashboard.
// Wire real sensors by enabling them individually (USE_SHT30 / USE_DHT / USE_PIR / USE_CO2),
// so a board with only one of them still reports honestly. Telemetry marks "tempReal" so the
// engine only pins zone physics to measured temperatures, never to placeholders.
// SHT30 (temp/RH) and ACD1200 (CO2) share one I2C bus; see the level-shifter warning below.
// -----------------------------------------------------------------------------

#include <Arduino.h>
#include <WiFi.h>
#include <PubSubClient.h>
#include <ArduinoJson.h>

// ---------------- CONFIG ----------------
// WiFi + broker address for THIS site live in src/wifi_secrets.h, which is gitignored
// so credentials never reach the repo: copy src/wifi_secrets.example.h over and fill
// it in (defines WIFI_SSID, WIFI_PASS, MQTT_HOST).
#include "wifi_secrets.h"
const int   MQTT_PORT = 1883;

// Per-board identity. These MUST differ between boards: the topic suffix is what the
// engine binds to a zone, so two nodes sharing one would interleave their telemetry into
// the same zone and fight over its commands.
//
// Both are overridable at build time so one source tree flashes a whole floor without
// editing this file per device — which is what platformio.ini and the README have always
// documented. (They documented it before it worked: the override was in the docs and the
// example build_flags, but nothing here read it, so every board flashed by following the
// instructions came up as "zone_1" and collided.)
//
//   build_flags = -DZONE_TOPIC_OVERRIDE=\"zone_2\" -DZONE_LABEL_OVERRIDE=\"Level 5\"
#ifndef ZONE_TOPIC_OVERRIDE
  #define ZONE_TOPIC_OVERRIDE "zone_1"
#endif
#ifndef ZONE_LABEL_OVERRIDE
  #define ZONE_LABEL_OVERRIDE "Level 4"
#endif
const char* ZONE_LABEL    = ZONE_LABEL_OVERRIDE;   // human label sent in telemetry
const char* ZONE_TOPIC    = ZONE_TOPIC_OVERRIDE;   // topic suffix; engine maps this to a zoneId
// Derived topics
char TELEMETRY_TOPIC[48];                   // econ/telemetry/<ZONE_TOPIC>
char COMMAND_TOPIC[48];                     // econ/commands/<ZONE_TOPIC>
char STATUS_TOPIC[48];                      // econ/status/<ZONE_TOPIC>  (LWT online/offline)
char CLIENT_ID[32];                         // econ-esp32-<ZONE_TOPIC>

// ---------------- HARDWARE PINS ----------------
const int RELAY_PIN  = 23;  // lighting relay (active HIGH)
// GPIO19, NOT GPIO22: 22 is the I2C clock. applyHvacSetpoint() pulses this pin, so leaving
// the emitter on 22 made every setpoint command drive SCL directly and corrupt any read from
// the SHT30 or the ACD1200 sharing that bus.
const int IR_PIN     = 19;  // HVAC IR emitter (see applyHvacSetpoint)
const int STATUS_LED = 2;   // onboard LED = MQTT link status

// ---------------- SENSORS ----------------
// Wire only what you actually have: each sensor is enabled independently, and NOTHING is
// ever fabricated. A sensor that is absent or fails its read omits its field entirely and
// the engine keeps modelling that quantity itself (mqtt.go uses pointer fields, so
// "not reported" is distinct from a real zero). This matters — a DHT22 drops reads
// routinely, and substituting a plausible-looking constant while still claiming
// tempReal:true would pin the zone's physics, and its AFDD residual, to a number no
// sensor ever measured.
//
// USE_REAL_SENSORS=1 remains supported as shorthand for "DHT + PIR" (older build_flags).
#ifndef USE_REAL_SENSORS
  #define USE_REAL_SENSORS 0
#endif
#ifndef USE_SHT30
  #define USE_SHT30 0                // SHT30 over I2C -> measured temperature + humidity
#endif
#ifndef USE_DHT
  // DHT is the fallback path; SHT30 wins when both are compiled in.
  #define USE_DHT (USE_REAL_SENSORS && !USE_SHT30)
#endif
#ifndef USE_PIR
  #define USE_PIR USE_REAL_SENSORS   // PIR   -> measured presence
#endif
#ifndef USE_CO2
  #define USE_CO2 0                  // ASAIR ACD1200 NDIR (I2C) -> measured CO2 ppm
#endif
#ifndef USE_MMWAVE
  #define USE_MMWAVE 0               // HLK-LD2410C radar -> presence incl. stationary people
#endif
#ifndef USE_PLUG
  // Plug-load node (APLC): SCT-013 current clamp -> measured plug-circuit watts, plus a
  // second relay that switches the zone's non-critical socket circuit. This is the load a
  // conventional BMS neither meters nor controls — 26.4% of energy in the Hanoi office
  // case study — and the reason this node exists.
  #define USE_PLUG 0
#endif

// ---------------------------------------------------------------------------------
// Sensors that replace a MODELLED value in the twin with a measured one.
//
// Each of the three below exists because the engine currently substitutes an assumption
// where a number should be, and the assumption is load-bearing for something the twin
// claims. They are separate flags so a board fitted with one still reports honestly
// about the other two.
// ---------------------------------------------------------------------------------
#ifndef USE_SUPPLY_TEMP
  // DS18B20 (1-Wire) cable-tied in the indoor unit's discharge louvre.
  //
  // simulation/dynamics.go regresses cooling against flow x (T_room - T_supply) and takes
  // T_supply from a CONSTANT (supplyAirDesignC, 12 C). A split unit's discharge is
  // nowhere near 12 C and it moves with compressor state, so the cooling-authority
  // coefficient — the one the learned setback ceiling depends on — is fit against a
  // number nobody measured. This probe replaces it. Its gap to room temperature is also
  // a far better answer to "is the compressor actually running" than acReal, which only
  // reports that the firmware SENT an IR frame.
  #define USE_SUPPLY_TEMP 0
#endif
#ifndef USE_AC_CLAMP
  // Second SCT-013 on the indoor unit's own supply, GPIO35 (ADC1, input-only).
  //
  // The same regressor takes `flow` from the twin's SIMULATED VAV. A real room on a split
  // AC has no VAV at all. The compressor's power draw IS the cooling drive term, so
  // clamping it turns that regressor from a simulation artifact into a measurement.
  #define USE_AC_CLAMP 0
#endif
#ifndef USE_LUX
  // BH1750 ambient light (I2C 0x23), facing the facade.
  //
  // Solar gain in the engine is `solarGainMultiplier x a constant` — a static per-zone
  // number with no time-of-day or cloud response, so a west facade behaves identically at
  // 08:00 and 16:00. A lux reading is a real irradiance proxy, and it is also what makes
  // daylight-linked dimming possible rather than assumed.
  #define USE_LUX 0
#endif

#if USE_AC_CLAMP
  #ifndef AC_CLAMP_PIN
    #define AC_CLAMP_PIN 35
  #endif
  #ifndef AC_CAL_A_PER_V
    #define AC_CAL_A_PER_V 60.6f   // same burden/front end as the plug clamp
  #endif
  #ifndef AC_MAINS_V
    #define AC_MAINS_V 220.0f
  #endif
#endif

#if USE_SUPPLY_TEMP
  #include <OneWire.h>
  #include <DallasTemperature.h>
  #ifndef SUPPLY_TEMP_PIN
    #define SUPPLY_TEMP_PIN 26     // clear of I2C, the IR pin and both relay lines
  #endif
  static OneWire supplyWire(SUPPLY_TEMP_PIN);
  static DallasTemperature supplyProbe(&supplyWire);
  static bool supplyProbeReady = false;
#endif

#if USE_LUX
  #ifndef BH1750_ADDR
    #define BH1750_ADDR 0x23
  #endif
  static bool luxReady = false;
#endif

#if USE_PLUG
  // SCT-013 on GPIO34: ADC1 channel 6, input-only. Deliberate — ADC2 is unusable while
  // WiFi is up, and an input-only pin can never be misconfigured into driving the clamp.
  // Wiring (SCT-013-000, 100 A : 50 mA current-output variant): burden resistor ~33 Ω
  // across the jack, DC bias the signal to 3.3 V/2 with a 10k/10k divider + 10 µF, tip
  // to GPIO34. The -030 (1 V voltage-output) variant needs only the bias divider.
  #ifndef PLUG_ADC_PIN
    #define PLUG_ADC_PIN 34
  #endif
  // Plug-circuit relay (active HIGH), separate from the lighting relay on GPIO23. Boots
  // ON: a reboot must never leave the room's sockets dead (fail-energized, like a BMS).
  #ifndef PLUG_RELAY_PIN
    #define PLUG_RELAY_PIN 25
  #endif
  // Calibration: amps of primary current per volt at the ADC. 100 A / (0.05 A × 33 Ω)
  // ≈ 60.6 for the -000 variant with a 33 Ω burden; 30.0 for the -030 (30 A / 1 V).
  #ifndef PLUG_CAL_A_PER_V
    #define PLUG_CAL_A_PER_V 60.6
  #endif
  #ifndef PLUG_MAINS_V
    #define PLUG_MAINS_V 230.0       // Vietnam single-phase nominal
  #endif
#endif

#if USE_SHT30 || USE_CO2
  #include <Wire.h>
  #ifndef I2C_SDA
    #define I2C_SDA 21
  #endif
  #ifndef I2C_SCL
    #define I2C_SCL 22
  #endif

  // A pin collision on the I2C bus is silent at runtime and looks like a flaky sensor, so
  // it is caught here instead. Overriding I2C_SDA/I2C_SCL onto an actuator pin fails the build.
  #if I2C_SDA == 23 || I2C_SCL == 23
    #error "I2C pin collides with RELAY_PIN (GPIO23) - pick another pin via -DI2C_SDA/-DI2C_SCL"
  #endif
  #if I2C_SDA == 19 || I2C_SCL == 19
    #error "I2C pin collides with IR_PIN (GPIO19) - pick another pin via -DI2C_SDA/-DI2C_SCL"
  #endif
  #if USE_PLUG && (I2C_SDA == 25 || I2C_SCL == 25)
    #error "I2C pin collides with PLUG_RELAY_PIN (GPIO25) - pick another pin via -DI2C_SDA/-DI2C_SCL"
  #endif

  // CRC-8, polynomial 0x31, init 0xFF. Shared: the SHT30 (datasheet 4.12) and the ASAIR
  // ACD1200 (datasheet 2.3.1) specify exactly the same check, so both sensors validate
  // through one routine. Every word is checksummed; we verify all of them, because a corrupt
  // reading published as measured is worse than no reading at all.
  static uint8_t crc8_31(uint8_t msb, uint8_t lsb) {
    uint8_t crc = 0xFF;
    uint8_t bytes[2] = {msb, lsb};
    for (int i = 0; i < 2; i++) {
      crc ^= bytes[i];
      for (int b = 0; b < 8; b++) crc = (crc & 0x80) ? (uint8_t)((crc << 1) ^ 0x31) : (uint8_t)(crc << 1);
    }
    return crc;
  }
#endif

#if USE_SHT30
  // Sensirion SHT30 on I2C (default address 0x44; ADDR pin high -> 0x45). Preferred over the
  // DHT family for HVAC work: +/-0.2 C beats a DHT11's +/-2 C, which is meaningless against a
  // 2 C control deadband, and I2C avoids the DHT's timing-critical single-wire protocol.
  #ifndef SHT30_ADDR
    #define SHT30_ADDR 0x44
  #endif

  // Single-shot, high repeatability, clock stretching disabled (0x2400). Returns false on a
  // bus error or a failed checksum, so the caller omits the field instead of inventing one.
  bool readSht30(float &t, float &h) {
    Wire.beginTransmission(SHT30_ADDR);
    Wire.write(0x24); Wire.write(0x00);
    if (Wire.endTransmission() != 0) return false;
    delay(20);                                   // >= 15 ms conversion at high repeatability
    if (Wire.requestFrom(SHT30_ADDR, 6) != 6) return false;
    uint8_t d[6];
    for (int i = 0; i < 6; i++) d[i] = Wire.read();
    if (crc8_31(d[0], d[1]) != d[2]) return false;
    if (crc8_31(d[3], d[4]) != d[5]) return false;
    uint16_t rawT = ((uint16_t)d[0] << 8) | d[1];
    uint16_t rawH = ((uint16_t)d[3] << 8) | d[4];
    t = -45.0f + 175.0f * ((float)rawT / 65535.0f);
    h = 100.0f * ((float)rawH / 65535.0f);
    return t > -40.0f && t < 125.0f && h >= 0.0f && h <= 100.0f;
  }
#endif

#if USE_DHT
  #include <DHT.h>
  #ifndef DHT_PIN
    #define DHT_PIN 4
  #endif
  #ifndef DHT_TYPE
    #define DHT_TYPE DHT22           // hshop.vn stocks DHT11, not DHT22: build -DDHT_TYPE=DHT11
  #endif
  DHT dht(DHT_PIN, DHT_TYPE);
#endif

#if USE_PIR
  #ifndef PIR_PIN
    #define PIR_PIN 5
  #endif
#endif

#if USE_MMWAVE
  // 24 GHz FMCW presence radar. A PIR detects *motion* and infers presence, so a person
  // typing quietly at a desk reads as an empty room within minutes — the classic "lights
  // drop on a full meeting" failure. The radar resolves micro-movement down to breathing
  // and holds its output high for a genuinely stationary person, which makes it the
  // honest presence sensor for an office. Verified-compatible modules (same contract,
  // "output high level when sensing", per each maker's spec):
  //   HLK-LD2410C  — OUT pin, 5 V supply, 3.3 V logic out
  //   Ai-Thinker Rd-03 / Rd-03_V2 — OT2 pin (pin 5), 3.3 V supply (3.0-3.6 V)
  // Either wires straight to the GPIO with no level shifter. The modules' UART side is
  // only for tuning gates/thresholds and is not needed here.
  #ifndef MMWAVE_PIN
    #define MMWAVE_PIN 18
  #endif
#endif

#if USE_CO2
  // ASAIR ACD1200 NDIR CO2 sensor, I2C mode — shares the SHT30's bus. (MH-Z19 is not sold in
  // Vietnam; the ACD1200 is the stocked NDIR part.) The sensor defaults to I2C with Pin5 (SET)
  // left floating; pulling Pin5 low selects UART instead, which runs at 1200 baud and is not
  // worth the wire. Range 400-5000 ppm, +/-(50 ppm + 5% of reading), 120 s preheat.
  //
  // WIRING WARNING (datasheet 2.2): the ACD1200's I2C lines are internally pulled up to *5 V*.
  // The ESP32's GPIOs are 3.3 V and are not 5 V tolerant, so this sensor needs a bidirectional
  // I2C level shifter between it and the board — do not wire it straight to SDA/SCL.
  #ifndef ACD1200_ADDR
    #define ACD1200_ADDR 0x2A          // 7-bit; the datasheet writes it as 0x54 = 0x2A << 1
  #endif

  // Reads gas concentration (command 0x0300). Uplink is 9 bytes:
  //   PPM3 PPM2 CRC | PPM1 PPM0 CRC | TEMP1 TEMP0 CRC   (every two bytes followed by a CRC)
  // Concentration is assembled MSB-first across the two CRC-checked word pairs. Returns false
  // on a bus error or any failed checksum, so the caller omits "co2" rather than inventing an
  // air-quality reading the building never measured.
  bool readCo2(int &ppm) {
    Wire.beginTransmission(ACD1200_ADDR);
    Wire.write(0x03); Wire.write(0x00);
    if (Wire.endTransmission() != 0) return false;
    delay(5);
    if (Wire.requestFrom(ACD1200_ADDR, 9) != 9) return false;
    uint8_t d[9];
    for (int i = 0; i < 9; i++) d[i] = Wire.read();
    if (crc8_31(d[0], d[1]) != d[2]) return false;   // PPM3 PPM2
    if (crc8_31(d[3], d[4]) != d[5]) return false;   // PPM1 PPM0
    uint32_t v = ((uint32_t)d[0] << 24) | ((uint32_t)d[1] << 16) | ((uint32_t)d[3] << 8) | (uint32_t)d[4];
    ppm = (int)v;
    // Datasheet range is 400-5000 ppm; allow a little slack either side but reject the zeros
    // and garbage the sensor emits during its 120 s preheat.
    return ppm >= 300 && ppm <= 10000;
  }

  // The ACD1200 ships in AUTOMATIC baseline calibration (datasheet 1.1): 24 h after
  // power-on and every 7 days after, it re-zeroes by assuming the lowest concentration it
  // saw recently was outdoor air. In an office that empties overnight that is free and
  // correct. In a continuously occupied space — a server room, a 24/7 floor — the room
  // never gives it that outdoor hour, so the sensor quietly drags its baseline up to an
  // occupied reading and under-reports from then on while looking perfectly healthy.
  // Build with -DCO2_ABC_OFF=1 for such spaces: at boot the node switches the sensor to
  // MANUAL calibration and verifies the write. Manual mode trades silent drift for a
  // maintenance task (single-point calibration, command 0x5204, in known air).
  #ifndef CO2_ABC_OFF
    #define CO2_ABC_OFF 0
  #endif
  #if CO2_ABC_OFF
  bool co2DisableAutoCal() {
    // Set calibration mode = manual: 0x5306 + data 0x0000 + CRC over the data bytes
    // (datasheet table 7; its own example CRC 0x81 = crc8_31(0x00,0x00), validated).
    Wire.beginTransmission(ACD1200_ADDR);
    Wire.write(0x53); Wire.write(0x06);
    Wire.write(0x00); Wire.write(0x00);
    Wire.write(crc8_31(0x00, 0x00));
    if (Wire.endTransmission() != 0) return false;
    delay(10);                                   // datasheet: >5 ms before the readback
    // Read the mode back (tables 8/9): the set command alone proves nothing, so require
    // a CRC-valid response whose low data byte says manual (0 = manual, 1 = automatic).
    Wire.beginTransmission(ACD1200_ADDR);
    Wire.write(0x53); Wire.write(0x06);
    if (Wire.endTransmission() != 0) return false;
    delay(5);
    if (Wire.requestFrom(ACD1200_ADDR, 3) != 3) return false;
    uint8_t d[3];
    for (int i = 0; i < 3; i++) d[i] = Wire.read();
    if (crc8_31(d[0], d[1]) != d[2]) return false;
    return d[1] == 0x00;
  }
  #endif
#endif

// Zero-wiring presence demo (ignored when a real presence sensor is compiled in): T9 = GPIO32 capacitive
// touch. Touching the bare pin (or a jumper wire in it) drops the reading well below
// the boot-time baseline -> occupied. Publishes immediately on change for a snappy demo.
#define USE_TOUCH_PRESENCE 1
#if !USE_PIR && !USE_MMWAVE && USE_TOUCH_PRESENCE
  const int TOUCH_PIN = T9;      // GPIO32
  const int TOUCH_OCCUPANTS = 3; // headcount reported while touched
  int touchBaseline = 0;         // calibrated in setup()
  bool touchState = false;       // debounced presence state
  // A single threshold flaps when a bare-finger reading hovers right at the line
  // (observed live: occupancy oscillating 3<->0 while held). Hysteresis — enter well
  // below baseline, leave only after recovering most of the way — plus a requirement
  // of 3 consecutive agreeing samples makes the state boringly stable.
  bool touchOccupied() {
    static int agree = 0;
    int v = touchRead(TOUCH_PIN);
    bool raw = touchState ? (v < touchBaseline * 82 / 100)   // stay until clearly released
                          : (v < touchBaseline * 62 / 100);  // enter only on a firm touch
    if (raw == touchState) { agree = 0; return touchState; }
    if (++agree >= 3) { agree = 0; touchState = raw; }
    return touchState;
  }
#endif

WiFiClient   espClient;
PubSubClient client(espClient);

unsigned long lastPublish = 0;
const long PUBLISH_INTERVAL_MS = 5000;
unsigned long lastReconnectAttempt = 0;

// actuated state (echoed back in telemetry for diagnostics)
bool  lightsOn = true;
float hvacSetpointC = 24.0;

// ---------------- WIFI ----------------
void setupWifi() {
  WiFi.mode(WIFI_STA);
  WiFi.begin(WIFI_SSID, WIFI_PASS);
  Serial.printf("[wifi] connecting to %s", WIFI_SSID);
  while (WiFi.status() != WL_CONNECTED) { delay(400); Serial.print("."); }
  Serial.printf("\n[wifi] connected, ip=%s\n", WiFi.localIP().toString().c_str());
}

// ---------------- HVAC IR ----------------
// This is where the whole control loop terminates: the engine's optimizer decides a
// setpoint, publishes it, and the node has to make a real air conditioner obey.
//
// For a long time it did not. applyHvacSetpoint() pulsed the pin three times and logged
// the setpoint, which looks convincing on a scope and in the serial monitor while the AC
// does absolutely nothing — every setback the twin reported saving energy on was, at this
// node, a no-op. USE_IR_AC=1 now sends a genuine per-brand IR frame via IRremoteESP8266,
// and telemetry reports which of the two is actually happening (`acReal`), so a bare demo
// board can never be mistaken for a node that is really driving a machine.
#ifndef USE_IR_AC
  #define USE_IR_AC 0
#endif

#if USE_IR_AC
  #include <IRremoteESP8266.h>
  #include <IRsend.h>
  #include <IRac.h>

  // Which brand's protocol to speak. IRremoteESP8266's IRac wraps ~50 vendor protocols
  // behind one state struct, so switching brands is a build flag, not a rewrite:
  //   -DIR_AC_PROTOCOL=COOLIX     generic; many budget/OEM splits (a good first try)
  //   -DIR_AC_PROTOCOL=DAIKIN     -DIR_AC_PROTOCOL=PANASONIC_AC
  //   -DIR_AC_PROTOCOL=MITSUBISHI_AC   -DIR_AC_PROTOCOL=LG2   -DIR_AC_PROTOCOL=SAMSUNG_AC
  //   -DIR_AC_PROTOCOL=TOSHIBA_AC -DIR_AC_PROTOCOL=GREE       (Casper/Nagakawa are often GREE)
  // If the unit does not respond, capture its remote with IRremoteESP8266's IRrecvDumpV3
  // example and use whatever protocol it decodes to.
  #ifndef IR_AC_PROTOCOL
    #define IR_AC_PROTOCOL COOLIX
  #endif
  #ifndef IR_AC_MODEL
    #define IR_AC_MODEL 1            // vendor sub-model; 1 is the default for most
  #endif
  #ifndef IR_AC_FAN
    #define IR_AC_FAN kAuto          // stdAc::fanspeed_t member name
  #endif

  IRac ac(IR_PIN);
  bool irAcReady = false;            // protocol compiled AND supported by the library
#endif

// applyHvacSetpoint drives the room's air conditioner to `celsius`.
//
// Bounds are enforced here rather than trusted from the wire: a corrupt or malicious
// SETPOINT= would otherwise be handed straight to the AC. 16-30 °C is the range essentially
// every split unit accepts, and clamping is safer than refusing (a refused command leaves
// the room uncontrolled, a clamped one is merely conservative).
void applyHvacSetpoint(float celsius) {
  if (isnan(celsius) || celsius < 16.0f || celsius > 30.0f) {
    Serial.printf("[hvac] setpoint %.1f C out of range -> clamped\n", celsius);
    celsius = celsius < 16.0f ? 16.0f : (celsius > 30.0f ? 30.0f : 24.0f);
  }
  hvacSetpointC = celsius;

#if USE_IR_AC
  if (irAcReady) {
    // The AC's full desired state, not just a temperature: these units are stateless
    // receivers — each frame carries mode, fan and power as well, so omitting them would
    // let the machine fall back to whatever it last heard from the handset.
    ac.next.protocol = decode_type_t::IR_AC_PROTOCOL;
    ac.next.model    = IR_AC_MODEL;
    ac.next.mode     = stdAc::opmode_t::kCool;
    ac.next.celsius  = true;
    ac.next.degrees  = celsius;
    ac.next.fanspeed = stdAc::fanspeed_t::IR_AC_FAN;
    ac.next.swingv   = stdAc::swingv_t::kOff;
    ac.next.swingh   = stdAc::swingh_t::kOff;
    ac.next.light    = false;
    ac.next.beep     = false;
    ac.next.econo    = false;
    ac.next.filter   = false;
    ac.next.turbo    = false;
    ac.next.quiet    = false;
    ac.next.clean    = false;
    ac.next.sleep    = -1;
    ac.next.clock    = -1;
    ac.next.power    = true;
    ac.sendAc();
    Serial.printf("[hvac] IR frame sent: %s -> %.1f C\n",
                  typeToString(decode_type_t::IR_AC_PROTOCOL).c_str(), celsius);
    return;
  }
  Serial.println("[hvac] WARNING: IR AC compiled in but protocol unsupported -> not sent");
#else
  // No AC control fitted. The pulse is a diagnostic only — it lets a scope or an LED on
  // IR_PIN confirm the command arrived and was parsed — and telemetry says acReal:false
  // so nothing downstream reads this as a machine having been commanded.
  Serial.printf("[hvac] setpoint %.1f C parsed (no IR AC compiled in; acReal:false)\n", celsius);
  for (int i = 0; i < 3; i++) { digitalWrite(IR_PIN, HIGH); delay(8); digitalWrite(IR_PIN, LOW); delay(8); }
#endif
}

void setLights(bool on) {
  lightsOn = on;
  digitalWrite(RELAY_PIN, on ? HIGH : LOW);
  Serial.printf("[relay] lights %s\n", on ? "ON" : "OFF");
}

#if USE_PLUG
bool plugOn = true;  // fail-energized: sockets are live until the engine says otherwise

void setPlug(bool on) {
  plugOn = on;
  digitalWrite(PLUG_RELAY_PIN, on ? HIGH : LOW);
  Serial.printf("[relay] plug circuit %s\n", on ? "ON" : "OFF");
}

// True-RMS current over ~100 ms (≈5 mains cycles at 50 Hz): sample fast, subtract the
// DC bias as the window mean, RMS the AC residue. Returns amps, or -1 when the window
// was starved of samples (then the field is omitted — never fabricated).
float readPlugAmps() {
  double sum = 0, sumSq = 0;
  int n = 0;
  unsigned long start = millis();
  while (millis() - start < 100) {
    int v = analogRead(PLUG_ADC_PIN);
    sum += v;
    sumSq += (double)v * v;
    n++;
  }
  if (n < 100) return -1;
  double mean = sum / n;
  double rmsCounts = sqrt(fmax(0.0, sumSq / n - mean * mean));
  float amps = (float)(rmsCounts * (3.3 / 4095.0) * PLUG_CAL_A_PER_V);
  return amps < 0.10 ? 0.0f : amps;  // below the clamp's noise floor = genuinely off
}
#endif

#if USE_AC_CLAMP
// True-RMS on the air conditioner's own supply. Identical front end and identical
// discipline to the plug clamp: a starved sampling window returns -1 and the field is
// omitted, because a fabricated zero here would tell the twin the compressor is off.
float readAcAmps() {
  double sum = 0, sumSq = 0;
  int n = 0;
  unsigned long start = millis();
  while (millis() - start < 100) {
    int v = analogRead(AC_CLAMP_PIN);
    sum += v;
    sumSq += (double)v * v;
    n++;
  }
  if (n < 100) return -1;
  double mean = sum / n;
  double rmsCounts = sqrt(fmax(0.0, sumSq / n - mean * mean));
  float amps = (float)(rmsCounts * (3.3 / 4095.0) * AC_CAL_A_PER_V);
  return amps < 0.10 ? 0.0f : amps;
}
#endif

#if USE_LUX
// BH1750 in one-time high-resolution mode: 1 lx resolution, ~120 ms conversion. One-shot
// rather than continuous so the part returns to low power between the node's 5 s cycles.
bool readLux(float& out) {
  Wire.beginTransmission(BH1750_ADDR);
  Wire.write(0x20);                       // one-time H-resolution mode
  if (Wire.endTransmission() != 0) return false;
  delay(180);                             // datasheet max conversion 180 ms
  if (Wire.requestFrom(BH1750_ADDR, 2) != 2) return false;
  uint16_t raw = (Wire.read() << 8) | Wire.read();
  out = raw / 1.2f;                       // datasheet counts -> lux
  return true;
}
#endif

#if USE_SUPPLY_TEMP
// DS18B20 in the supply-air stream. Returns false on the disconnect sentinel (-127 C)
// and on the power-on default (85 C) so a probe that has fallen out of the louvre is
// omitted rather than published as a plausible supply temperature.
bool readSupplyC(float& out) {
  if (!supplyProbeReady) return false;
  supplyProbe.requestTemperatures();
  float c = supplyProbe.getTempCByIndex(0);
  if (c <= -100.0f || c >= 84.9f) return false;
  out = c;
  return true;
}
#endif

// ---------------- COMMAND PARSING ----------------
// Accepts "LIGHTS_ON;SETPOINT=22.0", "LIGHTS_OFF;SETPOINT=26.0", or "HVAC_SET:23".
// Ignores any extra ;KEY=VAL tokens (e.g. the gateway's ;SRC=FAILSAFE).
void handleCommand(const String& msg) {
  int start = 0;
  while (start < (int)msg.length()) {
    int sep = msg.indexOf(';', start);
    String tok = (sep == -1) ? msg.substring(start) : msg.substring(start, sep);
    tok.trim();

    if (tok == "LIGHTS_ON")       setLights(true);
    else if (tok == "LIGHTS_OFF") setLights(false);
    else if (tok.startsWith("SETPOINT=")) applyHvacSetpoint(tok.substring(9).toFloat());
    else if (tok.startsWith("HVAC_SET:")) applyHvacSetpoint(tok.substring(9).toFloat());
#if USE_PLUG
    // After-hours sweep (APLC, plugs.go): the engine sheds the zone's non-critical
    // sockets on verified vacancy and restores them the instant presence returns.
    else if (tok == "PLUG_ON")  setPlug(true);
    else if (tok == "PLUG_OFF") setPlug(false);
#endif

    if (sep == -1) break;
    start = sep + 1;
  }
}

void onMessage(char* topic, byte* payload, unsigned int len) {
  String msg;
  msg.reserve(len);
  for (unsigned int i = 0; i < len; i++) msg += (char)payload[i];
  Serial.printf("[mqtt] %s -> %s\n", topic, msg.c_str());
  if (String(topic) == COMMAND_TOPIC) handleCommand(msg);
}

// ---------------- TELEMETRY ----------------
// Publishes only what was genuinely measured this cycle: any field whose sensor is absent
// or failed its read is omitted, so the engine keeps modelling it rather than trusting an
// invented number.
void readAndPublish() {
  StaticJsonDocument<256> doc;
  doc["zone"]   = ZONE_LABEL;
  doc["source"] = "esp32";

  // --- temperature + humidity ---
  bool tempReal = false;
#if USE_SHT30
  float t = NAN, h = NAN;
  if (readSht30(t, h)) {
    doc["temperature"] = round(t * 10) / 10.0;
    doc["humidity"]    = round(h * 10) / 10.0;
    tempReal = true;
  } else {
    Serial.println("[sht30] read/CRC failed -> omitted (engine keeps modelling)");
  }
#elif USE_DHT
  float t = dht.readTemperature();
  float h = dht.readHumidity();
  if (!isnan(t)) { doc["temperature"] = round(t * 10) / 10.0; tempReal = true; }
  if (!isnan(h)) { doc["humidity"]    = round(h * 10) / 10.0; }
  if (isnan(t)) Serial.println("[dht] temperature read failed -> omitted (engine keeps modelling)");
#else
  // Demo build with no temp/humidity sensor. The placeholder temperature is published
  // ONLY because tempReal:false travels with it — the engine sees the flag and models the
  // zone itself rather than pinning physics to the number.
  //
  // Humidity gets no such flag, and there is no humidityReal in the protocol, so a
  // simulated value here would arrive indistinguishable from a real one: the engine would
  // store it, stream it, and the dashboard would label it "Humidity (measured)". Omit it.
  // Absent is not zero — mqtt.go uses pointer fields precisely so this reads as
  // "no sensor is measuring humidity" rather than "the air is 0% RH".
  doc["temperature"] = round((22.0 + random(0, 40) / 10.0) * 10) / 10.0;  // simulated
#endif
  doc["tempReal"] = tempReal;  // only a genuine measurement may pin zone physics

  // --- occupancy ---
  int occupancy;
#if USE_PIR || USE_MMWAVE
  // Either sensor asserting means occupied. They fail in opposite directions — the PIR
  // misses a person sitting still, the radar can hold on residual micro-motion after an
  // exit — so OR-ing them errs toward "occupied", which for HVAC is the safe error: a
  // few minutes of extra cooling, never a dark room with someone in it.
  bool present = false;
  #if USE_PIR
    if (digitalRead(PIR_PIN) == HIGH) present = true;
  #endif
  #if USE_MMWAVE
    if (digitalRead(MMWAVE_PIN) == HIGH) present = true;
  #endif
  occupancy = present ? 1 : 0;  // presence, not a headcount
#elif USE_TOUCH_PRESENCE
  occupancy = touchOccupied() ? TOUCH_OCCUPANTS : 0;  // real physical input
#else
  occupancy = random(0, 6);
#endif
  doc["occupancy"] = occupancy;

  // --- CO2 ---
#if USE_CO2
  int ppm;
  if (readCo2(ppm)) doc["co2"] = ppm;
  else Serial.println("[co2] ACD1200 read/CRC failed -> omitted");
#else
  // No NDIR fitted: omit, for the same reason as humidity above. A number derived from
  // occupancy is a model output, and publishing it over the sensor channel would launder
  // it into "CO2 (measured)" on both dashboards. The engine already estimates CO2 from
  // occupancy itself — that estimate belongs there, where it is labelled as one.
#endif

  // --- plug circuit (APLC) ---
#if USE_PLUG
  float amps = readPlugAmps();
  if (amps >= 0) {
    doc["plugW"] = round(amps * PLUG_MAINS_V * 10) / 10.0;  // measured, engine-side model yields
  } else {
    Serial.println("[plug] ADC window starved -> omitted (engine keeps modelling)");
  }
  doc["plug"] = plugOn ? "ON" : "OFF";
#endif

  // --- measurements that replace a modelled value in the twin ---
  // Each is omitted when its sensor is absent or failed, never defaulted: the engine
  // keeps its own assumption and says so, which is the whole point of publishing these.
#if USE_SUPPLY_TEMP
  float supplyC;
  if (readSupplyC(supplyC)) doc["supplyC"] = round(supplyC * 10) / 10.0;
  else Serial.println("[supply] DS18B20 absent/out of range -> omitted (engine keeps its design value)");
#endif
#if USE_AC_CLAMP
  float acAmps = readAcAmps();
  if (acAmps >= 0) doc["acW"] = round(acAmps * AC_MAINS_V * 10) / 10.0;
  else Serial.println("[ac] ADC window starved -> omitted");
#endif
#if USE_LUX
  float lux;
  if (readLux(lux)) doc["lux"] = round(lux);
  else Serial.println("[lux] BH1750 read failed -> omitted");
#endif

  doc["lights"]      = lightsOn ? "ON" : "OFF";
  doc["setpoint"]    = hvacSetpointC;
  // Whether that setpoint was actually transmitted to an air conditioner, or merely
  // parsed. Same discipline as tempReal: the twin must never count energy saved by a
  // setback that terminated in a serial log line.
#if USE_IR_AC
  doc["acReal"] = irAcReady;
#else
  doc["acReal"] = false;
#endif

  char buf[288];
  size_t n = serializeJson(doc, buf);
  client.publish(TELEMETRY_TOPIC, buf, n);
  Serial.printf("[mqtt] pub %s -> %s\n", TELEMETRY_TOPIC, buf);
}

// ---------------- MQTT CONNECT ----------------
bool mqttConnect() {
  Serial.print("[mqtt] connecting...");
  // LWT: broker publishes "offline" (retained) on STATUS_TOPIC if this node drops.
  //
  // Credentials come from wifi_secrets.h (MQTT_USER / MQTT_PASS), because the shipped
  // broker refuses anonymous clients — econ/commands/<zone> closes a relay on a mains
  // circuit, and an open broker means anyone on the network can close it. Nodes without
  // the defines still build and connect anonymously, which only works against
  // mosquitto.dev.conf; rc=5 (unauthorized) is what a real broker answers instead.
#if defined(MQTT_USER) && defined(MQTT_PASS)
  bool ok = client.connect(CLIENT_ID, MQTT_USER, MQTT_PASS, STATUS_TOPIC, 0, true, "offline");
#else
  bool ok = client.connect(CLIENT_ID, nullptr, nullptr, STATUS_TOPIC, 0, true, "offline");
#endif
  if (ok) {
    Serial.println(" connected");
    client.publish(STATUS_TOPIC, "online", true);
    client.subscribe(COMMAND_TOPIC);
    digitalWrite(STATUS_LED, HIGH);
  } else {
    // rc=5 is "not authorized" — almost always a missing or wrong MQTT_USER/MQTT_PASS in
    // wifi_secrets.h against an authenticated broker. Naming it saves an hour of
    // suspecting the WiFi.
    Serial.printf(" failed rc=%d%s\n", client.state(),
                  client.state() == 5 ? " (not authorized - check MQTT_USER/MQTT_PASS)" : "");
    digitalWrite(STATUS_LED, LOW);
  }
  return ok;
}

void setup() {
  Serial.begin(115200);
  pinMode(RELAY_PIN, OUTPUT);
  pinMode(IR_PIN, OUTPUT);
#if USE_IR_AC
  // IRac has no begin() — the per-protocol sender initialises itself on send. What DOES
  // need doing is seeding the state struct to the library's defaults, so any field this
  // firmware does not set explicitly holds a sane value rather than whatever was on the
  // stack.
  IRac::initState(&ac.next);
  // Ask the library, rather than assuming: a protocol name can compile fine and still be
  // send-unsupported in this build of IRremoteESP8266, and finding that out at the first
  // setpoint command (silently) is exactly the failure this whole change is about.
  irAcReady = IRac::isProtocolSupported(decode_type_t::IR_AC_PROTOCOL);
  if (irAcReady) {
    Serial.printf("[hvac] IR AC control ACTIVE: %s on GPIO%d\n",
                  typeToString(decode_type_t::IR_AC_PROTOCOL).c_str(), IR_PIN);
  } else {
    Serial.printf("[hvac] WARNING: protocol %s is not send-supported by this library build; "
                  "setpoints will NOT reach the AC (acReal:false)\n",
                  typeToString(decode_type_t::IR_AC_PROTOCOL).c_str());
  }
#else
  Serial.printf("[hvac] no IR AC compiled in (build -DUSE_IR_AC=1); setpoints are parsed "
                "but not transmitted (acReal:false)\n");
#endif
  pinMode(STATUS_LED, OUTPUT);
  setLights(true);
#if USE_SHT30 || USE_CO2 || USE_LUX
  Wire.begin(I2C_SDA, I2C_SCL);
  Serial.printf("[i2c] bus up on SDA=GPIO%d SCL=GPIO%d\n", I2C_SDA, I2C_SCL);
#endif
#if USE_SHT30
  Serial.printf("[sht30] expecting I2C addr 0x%02X\n", SHT30_ADDR);
#endif
#if USE_DHT
  dht.begin();
  Serial.printf("[dht] sensor on GPIO%d\n", DHT_PIN);
#endif
#if USE_PIR
  pinMode(PIR_PIN, INPUT);
  Serial.printf("[pir] presence on GPIO%d\n", PIR_PIN);
#endif
#if USE_MMWAVE
  pinMode(MMWAVE_PIN, INPUT);
  Serial.printf("[mmwave] LD2410C presence on GPIO%d (holds for stationary occupants)\n", MMWAVE_PIN);
#endif
#if USE_CO2
  #if CO2_ABC_OFF
  if (co2DisableAutoCal()) {
    Serial.println("[co2] auto-calibration OFF, manual mode confirmed (24/7-occupied space)");
  } else {
    // Say so loudly: a 24/7 deployment that silently keeps ABC on will drift low.
    Serial.println("[co2] WARNING: could not confirm manual calibration; ABC remains ON");
  }
  #endif
  Serial.printf("[co2] ACD1200 expected at I2C 0x%02X (120 s preheat; needs a 5V<->3.3V level shifter)\n",
                ACD1200_ADDR);
#endif
#if USE_PLUG
  pinMode(PLUG_RELAY_PIN, OUTPUT);
  setPlug(true);  // fail-energized: sockets live from boot until the engine sheds them
  analogReadResolution(12);
  Serial.printf("[plug] SCT-013 on GPIO%d (cal %.1f A/V), relay on GPIO%d\n",
                PLUG_ADC_PIN, (double)PLUG_CAL_A_PER_V, PLUG_RELAY_PIN);
#endif
#if USE_SUPPLY_TEMP
  supplyProbe.begin();
  supplyProbeReady = supplyProbe.getDeviceCount() > 0;
  if (supplyProbeReady) {
    supplyProbe.setResolution(12);
    Serial.printf("[supply] DS18B20 on GPIO%d — measured supply air replaces the engine's "
                  "design constant for this zone\n", SUPPLY_TEMP_PIN);
  } else {
    Serial.printf("[supply] no DS18B20 found on GPIO%d — supplyC omitted, engine keeps its "
                  "design value\n", SUPPLY_TEMP_PIN);
  }
#endif
#if USE_AC_CLAMP
  analogReadResolution(12);
  Serial.printf("[ac] SCT-013 on the AC supply, GPIO%d (cal %.1f A/V) — measured compressor "
                "power replaces the twin's simulated VAV flow for this zone\n",
                AC_CLAMP_PIN, (double)AC_CAL_A_PER_V);
#endif
#if USE_LUX
  {
    float probe;
    luxReady = readLux(probe);
    Serial.printf("[lux] BH1750 at 0x%02X %s\n", BH1750_ADDR,
                  luxReady ? "responding — measured daylight replaces the static solar constant"
                           : "NOT responding — lux omitted");
  }
#endif
#if !USE_PIR && !USE_MMWAVE && USE_TOUCH_PRESENCE
  // Calibrate the untouched touch level; a touch reads far below this baseline.
  long acc = 0;
  for (int i = 0; i < 16; i++) { acc += touchRead(TOUCH_PIN); delay(10); }
  touchBaseline = acc / 16;
  Serial.printf("[touch] baseline=%d threshold=%d (GPIO32)\n", touchBaseline, touchBaseline * 6 / 10);
#endif

  snprintf(TELEMETRY_TOPIC, sizeof(TELEMETRY_TOPIC), "econ/telemetry/%s", ZONE_TOPIC);
  snprintf(COMMAND_TOPIC,   sizeof(COMMAND_TOPIC),   "econ/commands/%s",  ZONE_TOPIC);
  snprintf(STATUS_TOPIC,    sizeof(STATUS_TOPIC),    "econ/status/%s",    ZONE_TOPIC);
  snprintf(CLIENT_ID,       sizeof(CLIENT_ID),       "econ-esp32-%s",     ZONE_TOPIC);

  setupWifi();
  client.setServer(MQTT_HOST, MQTT_PORT);
  client.setCallback(onMessage);
}

void loop() {
  // Non-blocking reconnect (every 5s) keeps sensing/actuation responsive.
  if (!client.connected()) {
    digitalWrite(STATUS_LED, LOW);
    unsigned long now = millis();
    if (now - lastReconnectAttempt > 5000) {
      lastReconnectAttempt = now;
      mqttConnect();
    }
  } else {
    client.loop();
    unsigned long now = millis();
#if !USE_PIR && !USE_MMWAVE && USE_TOUCH_PRESENCE
    // Publish instantly when presence flips so the dashboard reacts in <0.2 s
    // instead of waiting out the periodic interval.
    static unsigned long lastTouchPoll = 0;
    static bool lastTouched = false;
    if (now - lastTouchPoll > 150) {
      lastTouchPoll = now;
      bool touched = touchOccupied();
      if (touched != lastTouched) {
        lastTouched = touched;
        lastPublish = now;
        readAndPublish();
      }
    }
#endif
    if (now - lastPublish > PUBLISH_INTERVAL_MS) {
      lastPublish = now;
      readAndPublish();
    }
  }
}
