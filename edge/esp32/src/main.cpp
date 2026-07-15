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
// Wire real sensors by enabling them individually (USE_DHT / USE_PIR / USE_CO2), so a
// board with only one of them still reports honestly. Telemetry marks "tempReal" so the
// engine only pins zone physics to measured temperatures, never to placeholders.
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

const char* ZONE_LABEL    = "Level 4";     // human label sent in telemetry
const char* ZONE_TOPIC    = "zone_1";      // topic suffix; engine maps this to a zoneId
// Derived topics
char TELEMETRY_TOPIC[48];                   // econ/telemetry/<ZONE_TOPIC>
char COMMAND_TOPIC[48];                     // econ/commands/<ZONE_TOPIC>
char STATUS_TOPIC[48];                      // econ/status/<ZONE_TOPIC>  (LWT online/offline)
char CLIENT_ID[32];                         // econ-esp32-<ZONE_TOPIC>

// ---------------- HARDWARE PINS ----------------
const int RELAY_PIN  = 23;  // lighting relay (active HIGH)
const int IR_PIN     = 22;  // HVAC IR emitter (see applyHvacSetpoint)
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
  #define USE_CO2 0                  // MH-Z19B NDIR -> measured CO2 ppm
#endif

#if USE_SHT30
  // Sensirion SHT30 on I2C (default address 0x44; ADDR pin high -> 0x45). Preferred over the
  // DHT family for HVAC work: +/-0.2 C beats a DHT11's +/-2 C, which is meaningless against a
  // 2 C control deadband, and I2C avoids the DHT's timing-critical single-wire protocol.
  #include <Wire.h>
  #ifndef SHT30_ADDR
    #define SHT30_ADDR 0x44
  #endif
  #ifndef SHT30_SDA
    #define SHT30_SDA 21
  #endif
  #ifndef SHT30_SCL
    #define SHT30_SCL 22
  #endif

  // CRC-8, polynomial 0x31, init 0xFF (SHT3x datasheet 4.12). The sensor checksums every
  // word; we verify both, because a corrupt reading published as measured is worse than none.
  static uint8_t sht30Crc(uint8_t msb, uint8_t lsb) {
    uint8_t crc = 0xFF;
    uint8_t bytes[2] = {msb, lsb};
    for (int i = 0; i < 2; i++) {
      crc ^= bytes[i];
      for (int b = 0; b < 8; b++) crc = (crc & 0x80) ? (uint8_t)((crc << 1) ^ 0x31) : (uint8_t)(crc << 1);
    }
    return crc;
  }

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
    if (sht30Crc(d[0], d[1]) != d[2]) return false;
    if (sht30Crc(d[3], d[4]) != d[5]) return false;
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
  const int PIR_OCCUPANTS = 1;  // a PIR senses presence, not a headcount
#endif

#if USE_CO2
  // MH-Z19B NDIR CO2 sensor on UART2 (9600 8N1). CO2_RX_PIN listens to the sensor's TX.
  // Give it ~3 min after power-on to reach datasheet accuracy.
  #ifndef CO2_RX_PIN
    #define CO2_RX_PIN 16
  #endif
  #ifndef CO2_TX_PIN
    #define CO2_TX_PIN 17
  #endif
  HardwareSerial co2Serial(2);

  // Frame checksum per the MH-Z19 datasheet: 0xFF - sum(bytes 1..7) + 1.
  uint8_t mhzChecksum(const uint8_t *p) {
    uint8_t s = 0;
    for (int i = 1; i < 8; i++) s += p[i];
    return (uint8_t)(0xFF - s + 1);
  }

  // Reads gas concentration. Returns false when the sensor does not answer in time or the
  // frame is corrupt — the caller then omits "co2" rather than inventing an air-quality
  // reading the building never measured.
  bool readCo2(int &ppm) {
    static const uint8_t cmd[9] = {0xFF, 0x01, 0x86, 0x00, 0x00, 0x00, 0x00, 0x00, 0x79};
    while (co2Serial.available()) co2Serial.read();  // drop any stale bytes
    co2Serial.write(cmd, 9);
    uint8_t r[9];
    int got = 0;
    unsigned long t0 = millis();
    while (got < 9 && millis() - t0 < 200) {
      if (co2Serial.available()) r[got++] = co2Serial.read();
    }
    if (got != 9 || r[0] != 0xFF || r[1] != 0x86) return false;
    if (r[8] != mhzChecksum(r)) return false;
    ppm = r[2] * 256 + r[3];
    return ppm > 0 && ppm < 10000;  // datasheet range sanity
  }
#endif

// Zero-wiring presence demo (ignored when USE_PIR=1): T9 = GPIO32 capacitive
// touch. Touching the bare pin (or a jumper wire in it) drops the reading well below
// the boot-time baseline -> occupied. Publishes immediately on change for a snappy demo.
#define USE_TOUCH_PRESENCE 1
#if !USE_PIR && USE_TOUCH_PRESENCE
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
// Sends the AC setpoint via IR. Real AC control needs the IRremoteESP8266 library
// and a per-brand protocol (Coolix/Daikin/etc.); this is the single extension point.
void applyHvacSetpoint(float celsius) {
  hvacSetpointC = celsius;
  Serial.printf("[hvac] IR -> setpoint %.1f C\n", celsius);
  // Visible pulse so a scope/LED on IR_PIN confirms a command was acted on.
  for (int i = 0; i < 3; i++) { digitalWrite(IR_PIN, HIGH); delay(8); digitalWrite(IR_PIN, LOW); delay(8); }
  // TODO(real AC): IRsendCoolix ac(IR_PIN); ac.send(buildCoolixState(celsius, lightsOn));
}

void setLights(bool on) {
  lightsOn = on;
  digitalWrite(RELAY_PIN, on ? HIGH : LOW);
  Serial.printf("[relay] lights %s\n", on ? "ON" : "OFF");
}

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
  doc["temperature"] = round((22.0 + random(0, 40) / 10.0) * 10) / 10.0;  // simulated
  doc["humidity"]    = 40.0 + random(0, 20);                              // simulated
#endif
  doc["tempReal"] = tempReal;  // only a genuine measurement may pin zone physics

  // --- occupancy ---
  int occupancy;
#if USE_PIR
  occupancy = digitalRead(PIR_PIN) == HIGH ? PIR_OCCUPANTS : 0;
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
  else Serial.println("[co2] MH-Z19 read failed -> omitted");
#else
  doc["co2"] = 400 + occupancy * 120 + random(0, 60);  // simulated
#endif

  doc["lights"]      = lightsOn ? "ON" : "OFF";
  doc["setpoint"]    = hvacSetpointC;

  char buf[288];
  size_t n = serializeJson(doc, buf);
  client.publish(TELEMETRY_TOPIC, buf, n);
  Serial.printf("[mqtt] pub %s -> %s\n", TELEMETRY_TOPIC, buf);
}

// ---------------- MQTT CONNECT ----------------
bool mqttConnect() {
  Serial.print("[mqtt] connecting...");
  // LWT: broker publishes "offline" (retained) on STATUS_TOPIC if this node drops.
  bool ok = client.connect(CLIENT_ID, nullptr, nullptr, STATUS_TOPIC, 0, true, "offline");
  if (ok) {
    Serial.println(" connected");
    client.publish(STATUS_TOPIC, "online", true);
    client.subscribe(COMMAND_TOPIC);
    digitalWrite(STATUS_LED, HIGH);
  } else {
    Serial.printf(" failed rc=%d\n", client.state());
    digitalWrite(STATUS_LED, LOW);
  }
  return ok;
}

void setup() {
  Serial.begin(115200);
  pinMode(RELAY_PIN, OUTPUT);
  pinMode(IR_PIN, OUTPUT);
  pinMode(STATUS_LED, OUTPUT);
  setLights(true);
#if USE_SHT30
  Wire.begin(SHT30_SDA, SHT30_SCL);
  Serial.printf("[sht30] I2C addr 0x%02X on SDA=GPIO%d SCL=GPIO%d\n", SHT30_ADDR, SHT30_SDA, SHT30_SCL);
#endif
#if USE_DHT
  dht.begin();
  Serial.printf("[dht] sensor on GPIO%d\n", DHT_PIN);
#endif
#if USE_PIR
  pinMode(PIR_PIN, INPUT);
  Serial.printf("[pir] presence on GPIO%d\n", PIR_PIN);
#endif
#if USE_CO2
  co2Serial.begin(9600, SERIAL_8N1, CO2_RX_PIN, CO2_TX_PIN);
  Serial.printf("[co2] MH-Z19 on UART2 rx=GPIO%d tx=GPIO%d (~3 min warm-up for rated accuracy)\n",
                CO2_RX_PIN, CO2_TX_PIN);
#endif
#if !USE_PIR && USE_TOUCH_PRESENCE
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
#if !USE_PIR && USE_TOUCH_PRESENCE
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
