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
// Define USE_REAL_SENSORS=1 (and wire DHT22 + PIR) for fully measured readings.
// Telemetry marks "tempReal" so the engine only pins zone physics to measured
// temperatures, never to the simulated placeholders.
// -----------------------------------------------------------------------------

#include <Arduino.h>
#include <WiFi.h>
#include <PubSubClient.h>
#include <ArduinoJson.h>

// ---------------- CONFIG (edit these) ----------------
const char* WIFI_SSID = "YOUR_WIFI_SSID";
const char* WIFI_PASS = "YOUR_WIFI_PASSWORD";
const char* MQTT_HOST = "192.168.1.100";   // Raspberry Pi / broker LAN IP
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

#define USE_REAL_SENSORS 0
#if USE_REAL_SENSORS
  #include <DHT.h>
  #define DHT_PIN 4
  #define PIR_PIN 5
  DHT dht(DHT_PIN, DHT22);
#endif

// Zero-wiring presence demo (ignored when USE_REAL_SENSORS=1): T9 = GPIO32 capacitive
// touch. Touching the bare pin (or a jumper wire in it) drops the reading well below
// the boot-time baseline -> occupied. Publishes immediately on change for a snappy demo.
#define USE_TOUCH_PRESENCE 1
#if !USE_REAL_SENSORS && USE_TOUCH_PRESENCE
  const int TOUCH_PIN = T9;      // GPIO32
  const int TOUCH_OCCUPANTS = 3; // headcount reported while touched
  int touchBaseline = 0;         // calibrated in setup()
  bool touchOccupied() { return touchRead(TOUCH_PIN) < touchBaseline * 6 / 10; }
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
void readAndPublish() {
  float temperature, humidity;
  int   occupancy, co2;

#if USE_REAL_SENSORS
  temperature = dht.readTemperature();
  humidity    = dht.readHumidity();
  if (isnan(temperature)) temperature = 24.0;
  if (isnan(humidity))    humidity    = 50.0;
  occupancy = digitalRead(PIR_PIN) == HIGH ? 1 : 0;   // PIR = presence; swap for a counter for headcount
  co2 = 400 + occupancy * 120;                         // approx until a real CO2 sensor is wired
#else
  temperature = 22.0 + (random(0, 40) / 10.0);
  humidity    = 40.0 + random(0, 20);
  #if USE_TOUCH_PRESENCE
    occupancy = touchOccupied() ? TOUCH_OCCUPANTS : 0;  // real physical input
  #else
    occupancy = random(0, 6);
  #endif
  co2 = 400 + occupancy * 120 + random(0, 60);
#endif

  StaticJsonDocument<256> doc;
  doc["zone"]        = ZONE_LABEL;
  doc["occupancy"]   = occupancy;
  doc["temperature"] = round(temperature * 10) / 10.0;
  doc["humidity"]    = round(humidity * 10) / 10.0;
  doc["co2"]         = co2;
  doc["source"]      = "esp32";
  doc["tempReal"]    = USE_REAL_SENSORS ? true : false; // only measured temps may pin physics
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
#if USE_REAL_SENSORS
  dht.begin();
  pinMode(PIR_PIN, INPUT);
#elif USE_TOUCH_PRESENCE
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
#if !USE_REAL_SENSORS && USE_TOUCH_PRESENCE
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
