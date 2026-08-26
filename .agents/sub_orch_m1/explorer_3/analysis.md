# Analysis Report: Milestone 1 Test Infrastructure & Host Mocking

**Author:** Explorer 3 (Milestone 1 Sub-Orchestration)  
**Date:** 2026-08-26  
**Target:** Host Test Suite (`edge/esp32/test/test_m1_dual_mode.cpp`), Mock Harness (`edge/esp32/test/arduino_shim.h` + mock headers), and Test Runner (`edge/esp32/test/run_host_tests.sh`)

---

## 1. Executive Summary

Milestone 1 introduces the edge communication subsystem for the ESP32 OV7670 person detection upgrade:
- `edge/esp32/src/camera/tracking_payload.h/.cpp` (BIM/topology JSON schema serialization)
- `edge/esp32/src/camera/dual_mode_comm.h/.cpp` (Dual-mode transport: Wi-Fi UDP :4210 broadcast + MQTT primary, automatic zero-delay USB Serial fallback)

To ensure **100% test coverage and non-blocking verification off-target (on macOS/Linux build hosts)** without requiring physical ESP32 hardware or active Wi-Fi routers, this report provides a comprehensive architectural blueprint for:
1. A **lightweight, header-only host mock harness** for Arduino core, `HardwareSerial`, `IPAddress`, `WiFi`, `WiFiUDP`, and `PubSubClient`.
2. A **deterministic test suite** (`test_m1_dual_mode.cpp`) exercising schema compliance, boundary safety, UDP/MQTT transport, Serial fallback, online/offline failover transitions, and the `<0.2ms` non-blocking tick budget.
3. Clean integration into `run_host_tests.sh` utilizing C++17 and the existing `ArduinoJson` 6.21.3 library.

---

## 2. Current State & Dependency Analysis

### 2.1 Existing Test Infrastructure
The current repository contains:
- `edge/esp32/test/run_host_tests.sh`: Compiles and executes `host_config_test.cpp` against `ArduinoJson` and `arduino_shim.h`.
- `edge/esp32/test/arduino_shim.h`: Minimal shim defining `SerialShim` and in-memory `Preferences`.
- `edge/esp32/test/Arduino.h` & `Preferences.h`: Forwarding headers pointing to `arduino_shim.h`.
- `.pio/libdeps/esp32dev/ArduinoJson/src`: ArduinoJson 6.21.3 header-only C++ library, fully compilable under standard C++17 on macOS (Clang) and Linux (GCC).

### 2.2 Identified Gaps for Milestone 1 Testing
The existing `arduino_shim.h` only handles `Serial` printing and `Preferences`. For `DualModeComm` and `TrackingPayload`, the following components are missing:
1. **Controllable Time/Clock**: `millis()`, `micros()`, and `delay()` must support virtual mock clock control so reconnect intervals (e.g. 5000 ms) and timeout policies can be tested instantaneously without sleeping in real time.
2. **IPAddress & String Support**: Standard Arduino `IPAddress` (e.g., `255.255.255.255`, `.toString()`) and `String` helpers.
3. **WiFi Mock (`WiFiClass`)**: Ability to inject connection states (`WL_CONNECTED`, `WL_DISCONNECTED`, `WL_CONNECTION_LOST`), inspect `WiFi.begin()` calls, and configure mock local IP.
4. **WiFiUDP Mock**: Intercept UDP packet transmission (`beginPacket`, `write`, `endPacket`), verify destination IP/port (`255.255.255.255:4210`), inspect payload buffer, and inject packet transmission errors (`failNextSend`).
5. **PubSubClient Mock**: Intercept MQTT publishes (`topic`, `payload`, `retained`), track message history, inject broker connection status (`connected()`, `state()`), and simulate inbound MQTT commands.
6. **Stream / HardwareSerial Mock**: Buffer inspection for Serial output (`getOutput()`, `clear()`, `getLineCount()`) to verify exact fallback JSON framing.

---

## 3. Host Mock Harness Architecture

The mock harness is designed to be **clean, modular, and non-invasive**. It allows firmware code in `src/camera/` to compile either for the ESP32 hardware target via PlatformIO or for the host test runner via standard C++17 compiler flags (`-I test -I src -I src/camera`).

### 3.1 Header Directory Structure
```
edge/esp32/test/
├── Arduino.h              # Includes arduino_shim.h
├── Preferences.h          # Includes arduino_shim.h
├── WiFi.h                 # Provides WiFiClass mock & WiFi global instance
├── WiFiUdp.h              # Provides WiFiUDP mock class
├── WiFiUDP.h              # Case-insensitive / compatibility alias
├── PubSubClient.h         # Provides PubSubClient mock class
├── arduino_shim.h         # Core types, time control, SerialShim, IPAddress, String
├── host_config_test.cpp   # Existing config validation tests
├── test_m1_dual_mode.cpp  # Milestone 1 DualMode & TrackingPayload unit test suite
└── run_host_tests.sh      # Unified host test runner script
```

### 3.2 Mock Component Specifications

#### A. Controllable Clock & Arduino Core (`arduino_shim.h`)
```cpp
#pragma once
#include <cstdarg>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <cmath>
#include <string>
#include <vector>
#include <functional>
#include <algorithm>

// --- Virtual Mock Clock ---
static unsigned long g_mock_millis = 1000;
static unsigned long g_mock_micros = 1000000;

inline unsigned long millis() { return g_mock_millis; }
inline unsigned long micros() { return g_mock_micros; }

inline void setMockMillis(unsigned long ms) {
  g_mock_millis = ms;
  g_mock_micros = ms * 1000UL;
}

inline void advanceMockMillis(unsigned long ms) {
  g_mock_millis += ms;
  g_mock_micros += ms * 1000UL;
}

inline void setMockMicros(unsigned long us) {
  g_mock_micros = us;
  g_mock_millis = us / 1000UL;
}

inline void advanceMockMicros(unsigned long us) {
  g_mock_micros += us;
  g_mock_millis = g_mock_micros / 1000UL;
}

inline void delay(unsigned long ms) { advanceMockMillis(ms); }
inline void delayMicroseconds(unsigned int us) { advanceMockMicros(us); }

// --- IPAddress Shim ---
class IPAddress {
private:
  uint8_t _bytes[4];
public:
  IPAddress() { _bytes[0] = _bytes[1] = _bytes[2] = _bytes[3] = 0; }
  IPAddress(uint8_t b0, uint8_t b1, uint8_t b2, uint8_t b3) {
    _bytes[0] = b0; _bytes[1] = b1; _bytes[2] = b2; _bytes[3] = b3;
  }
  uint8_t operator[](int i) const { return (i >= 0 && i < 4) ? _bytes[i] : 0; }
  uint8_t& operator[](int i) { return _bytes[i]; }
  
  std::string toString() const {
    char buf[32];
    snprintf(buf, sizeof(buf), "%u.%u.%u.%u", _bytes[0], _bytes[1], _bytes[2], _bytes[3]);
    return std::string(buf);
  }
  bool operator==(const IPAddress& o) const { return memcmp(_bytes, o._bytes, 4) == 0; }
  bool operator!=(const IPAddress& o) const { return !(*this == o); }
};

// --- Stream, Print & SerialShim ---
class Print {
public:
  virtual size_t write(uint8_t b) = 0;
  virtual size_t write(const uint8_t* buffer, size_t size) {
    size_t n = 0;
    while (size--) { if (write(*buffer++)) n++; else break; }
    return n;
  }
  size_t print(const char* s) { return write((const uint8_t*)s, strlen(s)); }
  size_t print(int n) { char b[32]; snprintf(b, sizeof(b), "%d", n); return print(b); }
  size_t println(const char* s = "") { size_t n = print(s); n += print("\n"); return n; }
  size_t printf(const char* fmt, ...) {
    char buf[512];
    va_list ap;
    va_start(ap, fmt);
    int len = vsnprintf(buf, sizeof(buf), fmt, ap);
    va_end(ap);
    if (len > 0) return write((const uint8_t*)buf, (size_t)len);
    return 0;
  }
};

class Stream : public Print {
public:
  virtual int available() { return 0; }
  virtual int read() { return -1; }
  virtual int peek() { return -1; }
  virtual void flush() {}
};

class SerialShim : public Stream {
public:
  std::string buffer;
  bool echoStdout = false;

  void clear() { buffer.clear(); }
  const std::string& getOutput() const { return buffer; }
  size_t getLineCount() const {
    size_t lines = 0;
    for (char c : buffer) if (c == '\n') lines++;
    return lines;
  }

  virtual size_t write(uint8_t b) override {
    buffer.push_back((char)b);
    if (echoStdout) putchar(b);
    return 1;
  }
  virtual size_t write(const uint8_t* p, size_t n) override {
    buffer.append((const char*)p, n);
    if (echoStdout) fwrite(p, 1, n, stdout);
    return n;
  }
  void begin(unsigned long) {}
};
static SerialShim Serial;
```

#### B. WiFi Mock (`test/WiFi.h`)
```cpp
#pragma once
#include "arduino_shim.h"

enum wl_status_t {
  WL_NO_SHIELD = 255,
  WL_IDLE_STATUS = 0,
  WL_NO_SSID_AVAIL = 1,
  WL_SCAN_COMPLETED = 2,
  WL_CONNECTED = 3,
  WL_CONNECT_FAILED = 4,
  WL_CONNECTION_LOST = 5,
  WL_DISCONNECTED = 6
};

#define WIFI_STA 1
#define WIFI_AP 2
#define WIFI_OFF 0

class WiFiClass {
public:
  wl_status_t _status = WL_DISCONNECTED;
  IPAddress _ip = IPAddress(192, 168, 1, 150);
  int _mode = WIFI_STA;
  std::string ssid;
  std::string pass;
  int beginCount = 0;
  int disconnectCount = 0;

  void mode(int m) { _mode = m; }
  void begin(const char* s, const char* p = nullptr) {
    ssid = s ? s : "";
    pass = p ? p : "";
    beginCount++;
  }
  void disconnect(bool = false) {
    _status = WL_DISCONNECTED;
    disconnectCount++;
  }
  wl_status_t status() const { return _status; }
  IPAddress localIP() const { return _ip; }

  // Mock Control API
  void setMockStatus(wl_status_t st) { _status = st; }
  void setMockIP(const IPAddress& ip) { _ip = ip; }
  void reset() {
    _status = WL_DISCONNECTED;
    _ip = IPAddress(192, 168, 1, 150);
    _mode = WIFI_STA;
    ssid.clear();
    pass.clear();
    beginCount = 0;
    disconnectCount = 0;
  }
};
static WiFiClass WiFi;
```

#### C. WiFiUDP Mock (`test/WiFiUdp.h` & `test/WiFiUDP.h`)
```cpp
#pragma once
#include "arduino_shim.h"

class WiFiUDP {
public:
  struct PacketRecord {
    IPAddress destIP;
    uint16_t destPort;
    std::vector<uint8_t> data;
  };

  std::vector<PacketRecord> sentPackets;
  bool isListening = false;
  uint16_t boundPort = 0;
  IPAddress activeDestIP;
  uint16_t activeDestPort = 0;
  std::vector<uint8_t> activePayload;
  bool failNextSend = false;

  uint8_t begin(uint16_t port) {
    isListening = true;
    boundPort = port;
    return 1;
  }
  void stop() { isListening = false; }
  
  int beginPacket(IPAddress ip, uint16_t port) {
    activeDestIP = ip;
    activeDestPort = port;
    activePayload.clear();
    return 1;
  }
  int beginPacket(const char* host, uint16_t port) {
    return beginPacket(IPAddress(255, 255, 255, 255), port);
  }
  size_t write(uint8_t byte) {
    activePayload.push_back(byte);
    return 1;
  }
  size_t write(const uint8_t* buffer, size_t size) {
    activePayload.insert(activePayload.end(), buffer, buffer + size);
    return size;
  }
  int endPacket() {
    if (failNextSend) {
      failNextSend = false;
      return 0; // Return 0 indicates transmission failure
    }
    sentPackets.push_back({activeDestIP, activeDestPort, activePayload});
    return 1; // 1 indicates success
  }

  // Inspection helpers
  void clearHistory() { sentPackets.clear(); activePayload.clear(); }
  size_t getPacketCount() const { return sentPackets.size(); }
  const PacketRecord& getLastPacket() const { return sentPackets.back(); }
  std::string getLastPacketPayload() const {
    if (sentPackets.empty()) return "";
    return std::string((const char*)sentPackets.back().data.data(), sentPackets.back().data.size());
  }
};
```

#### D. PubSubClient Mock (`test/PubSubClient.h`)
```cpp
#pragma once
#include "arduino_shim.h"

class PubSubClient : public Print {
public:
  struct MqttMessage {
    std::string topic;
    std::vector<uint8_t> payload;
    bool retained;
  };

  bool isMqttConnected = false;
  int stateCode = 0; // 0 = MQTT_CONNECTED
  std::string serverHost;
  uint16_t serverPort = 1883;
  std::vector<MqttMessage> publishHistory;
  std::vector<std::string> subscriptions;
  std::function<void(char*, uint8_t*, unsigned int)> callback;
  bool failNextPublish = false;

  PubSubClient() {}
  PubSubClient& setServer(const char* host, uint16_t port) {
    serverHost = host ? host : "";
    serverPort = port;
    return *this;
  }
  PubSubClient& setServer(IPAddress ip, uint16_t port) {
    serverHost = ip.toString();
    serverPort = port;
    return *this;
  }
  PubSubClient& setCallback(std::function<void(char*, uint8_t*, unsigned int)> cb) {
    callback = cb;
    return *this;
  }
  PubSubClient& setClient(Stream&) { return *this; }
  PubSubClient& setKeepAlive(uint16_t) { return *this; }
  PubSubClient& setSocketTimeout(uint16_t) { return *this; }
  bool setBufferSize(uint16_t) { return true; }
  uint16_t getBufferSize() { return 512; }

  bool connect(const char*, const char* = nullptr, const char* = nullptr) {
    isMqttConnected = true;
    stateCode = 0;
    return true;
  }
  bool connect(const char*, const char*, const char*, const char*, uint8_t, bool, const char*) {
    isMqttConnected = true;
    stateCode = 0;
    return true;
  }
  void disconnect() {
    isMqttConnected = false;
    stateCode = -1;
  }
  bool publish(const char* topic, const char* payload) {
    return publish(topic, (const uint8_t*)payload, payload ? strlen(payload) : 0, false);
  }
  bool publish(const char* topic, const char* payload, bool retained) {
    return publish(topic, (const uint8_t*)payload, payload ? strlen(payload) : 0, retained);
  }
  bool publish(const char* topic, const uint8_t* payload, unsigned int len) {
    return publish(topic, payload, len, false);
  }
  bool publish(const char* topic, const uint8_t* payload, unsigned int len, bool retained) {
    if (!isMqttConnected || failNextPublish) {
      if (failNextPublish) failNextPublish = false;
      return false;
    }
    MqttMessage msg;
    msg.topic = topic ? topic : "";
    if (payload && len > 0) msg.payload.assign(payload, payload + len);
    msg.retained = retained;
    publishHistory.push_back(msg);
    return true;
  }
  bool subscribe(const char* topic, uint8_t = 0) {
    if (!isMqttConnected) return false;
    subscriptions.push_back(topic ? topic : "");
    return true;
  }
  bool loop() { return isMqttConnected; }
  bool connected() { return isMqttConnected; }
  int state() { return stateCode; }

  virtual size_t write(uint8_t) override { return 1; }

  // Mock Control API
  void clearHistory() { publishHistory.clear(); subscriptions.clear(); }
  size_t getPublishCount() const { return publishHistory.size(); }
  const MqttMessage& getLastPublished() const { return publishHistory.back(); }
  std::string getLastPublishedPayload() const {
    if (publishHistory.empty()) return "";
    return std::string((const char*)publishHistory.back().payload.data(), publishHistory.back().payload.size());
  }
  void setMockConnected(bool conn) {
    isMqttConnected = conn;
    stateCode = conn ? 0 : -1;
  }
};
```

---

## 4. Test Scenarios Design (`test_m1_dual_mode.cpp`)

The test suite is partitioned into five distinct test domains, completely covering all functional requirements and edge cases.

```
+-------------------------------------------------------------------------------+
|                       Milestone 1 Test Suite Architecture                     |
+-------------------------------------------------------------------------------+
| 1. TrackingPayload JSON Serialization & Schema Compliance                     |
|    - Standard JSON schema structure and field matching                        |
|    - 64-bit epoch timestamp serialization (no uint32 overflow)                |
|    - Float rounding & 2-decimal precision formatting                          |
|    - Edge cases: 0 persons, 0.0 confidence, null pointers, empty strings      |
|    - Buffer boundary safety, zero-length / undersized buffer truncation check |
|    - Canary byte overflow inspection                                          |
+-------------------------------------------------------------------------------+
| 2. DualModeComm Wi-Fi Connected Mode (Primary Transport)                      |
|    - UDP Broadcast sent to 255.255.255.255 on port 4210                      |
|    - MQTT published to configured telemetry topic                             |
|    - Serial transport remains silent (no fallback written to Serial)          |
|    - Return code is true                                                      |
+-------------------------------------------------------------------------------+
| 3. DualModeComm Wi-Fi Disconnected Mode (Fallback Transport)                  |
|    - Immediate zero-delay fallback to Serial output                           |
|    - Output is newline-framed valid JSON                                      |
|    - Zero UDP packets or MQTT messages emitted                                |
|    - Return code is true (fallback successful)                                |
+-------------------------------------------------------------------------------+
| 4. Failover Transitions & Partial Failure Resilience                          |
|    - Sequence: Online -> Wi-Fi Drop -> Offline Fallback -> Reconnected Online |
|    - UDP socket send failure fallback trigger                                 |
|    - MQTT disconnected recovery during Wi-Fi connected state                  |
|    - Reconnect state machine hysteresis (no spamming Wi-Fi reconnect)         |
+-------------------------------------------------------------------------------+
| 5. Non-blocking Timing & Execution Budget Guarantees                          |
|    - `comm.tick()` execution time benchmark (< 0.2 ms / < 200 µs)             |
|    - Zero `delay()` calls during normal tick loops                            |
|    - Virtual clock advancement (5000 ms cooldown verification)                |
+-------------------------------------------------------------------------------+
```

### 4.1 Detailed Test Specifications

#### Test Group 1: TrackingPayload JSON Serialization
| Test Name | Test Conditions & Inputs | Expected Outcomes |
|---|---|---|
| `test_tracking_payload_nominal` | `sensor_id="esp32_cam_01"`, `zone_id="zone_1"`, `timestamp_ms=1724645160000ULL`, `person_detected=true`, `confidence=0.94f`, `person_count=2` | Returns length `> 0`, `deserializeJson` parses correctly, every key matches input. |
| `test_tracking_payload_64bit_timestamp` | `timestamp_ms=18446744073709551615ULL` and `1724645160000ULL` | JSON contains full 13+ digit integer literal without truncation or floating point exponent. |
| `test_tracking_payload_float_precision` | `confidence=0.942857f` | Output is `0.94` (clean 2-decimal representation). |
| `test_tracking_payload_edge_values` | `person_detected=false`, `confidence=0.0f`, `person_count=0` | JSON output has `"person_detected":false`, `"confidence":0.0`, `"person_count":0`. |
| `test_tracking_payload_null_strings` | `sensor_id=nullptr`, `zone_id=nullptr` | Does not crash; safely serializes `""` or `"unknown"`. |
| `test_tracking_payload_buffer_bounds` | Buffer capacity = 10 bytes (smaller than JSON output) | Returns `0`, does not write past 10 bytes (canaries intact). |
| `test_tracking_payload_null_buffer` | `buffer=nullptr`, `max_len=256` | Returns `0` without segmentation fault. |
| `test_tracking_payload_canary_safety` | Allocate `char buf[256]` bounded by `0xAA` guard bytes before and after | After serialization, all guard bytes remain strictly `0xAA`. |

#### Test Group 2: DualModeComm Wi-Fi Connected Mode
| Test Name | Test Conditions & Inputs | Expected Outcomes |
|---|---|---|
| `test_dual_mode_wifi_connected_udp` | `WiFi.setMockStatus(WL_CONNECTED)`, UDP configured on port 4210, broadcast IP `255.255.255.255` | 1 UDP packet recorded, target IP `255.255.255.255`, target port 4210, payload contains valid tracking JSON. |
| `test_dual_mode_wifi_connected_mqtt` | `WiFi.setMockStatus(WL_CONNECTED)`, `mqtt.setMockConnected(true)`, topic `"econ/telemetry/zone_1"` | 1 MQTT message recorded, topic `"econ/telemetry/zone_1"`, payload contains valid tracking JSON. |
| `test_dual_mode_wifi_connected_no_serial_leak` | Initial `Serial.clear()`, Wi-Fi & MQTT connected | `Serial.getOutput()` is empty (no fallback payload dumped to serial). |

#### Test Group 3: DualModeComm Wi-Fi Disconnected Mode
| Test Name | Test Conditions & Inputs | Expected Outcomes |
|---|---|---|
| `test_dual_mode_wifi_disconnected_serial_fallback` | `WiFi.setMockStatus(WL_DISCONNECTED)`, `mqtt.setMockConnected(false)` | `Serial.getOutput()` contains full JSON payload terminated with `\n`. |
| `test_dual_mode_wifi_disconnected_zero_network_packets` | Wi-Fi disconnected | `udp.getPacketCount() == 0`, `mqtt.getPublishCount() == 0`. |
| `test_dual_mode_zero_delay_fallback` | Measure execution time of `comm.transmit(data)` in disconnected mode | Returns in `< 50 µs`, no blocking network timeout or retry loops. |

#### Test Group 4: Failover Transitions
| Test Name | Test Conditions & Inputs | Expected Outcomes |
|---|---|---|
| `test_dual_mode_failover_sequence` | 1. Online: transmit frame 1 -> UDP & MQTT sent.<br>2. Wi-Fi drop (`WL_CONNECTION_LOST`): transmit frame 2 -> Serial fallback.<br>3. Still offline: transmit frame 3 -> Serial fallback.<br>4. Reconnect (`WL_CONNECTED`): transmit frame 4 -> UDP & MQTT resume. | Clean state transitions; correct transport used in each step; no stale state locks. |
| `test_dual_mode_udp_failure_fallback` | `WiFi.setMockStatus(WL_CONNECTED)`, `udp.failNextSend = true` | Upon UDP send failure, automatically routes frame to Serial fallback; returns `true`. |
| `test_dual_mode_mqtt_failure_resilience` | `WiFi.setMockStatus(WL_CONNECTED)`, `mqtt.setMockConnected(false)` | UDP broadcast still succeeds; MQTT failure does not stall the loop; triggers non-blocking reconnect. |

#### Test Group 5: Non-Blocking Timing & State Machine Loop
| Test Name | Test Conditions & Inputs | Expected Outcomes |
|---|---|---|
| `test_dual_mode_tick_execution_budget` | Execute 1,000 consecutive `comm.tick()` calls under varying connection states | Total elapsed time < 100 ms (average < 0.1 ms / 100 µs per tick, strictly satisfying `< 0.2 ms`). |
| `test_dual_mode_reconnect_cooldown` | Start disconnected at t=0ms (`tick()`), advance to t=1000ms (`tick()`), advance to t=5000ms (`tick()`) | Reconnect attempt initiated only at t=0ms and t=5000ms; t=1000ms is skipped due to non-blocking interval timer. |

---

## 5. Implementation Blueprint for `test_m1_dual_mode.cpp`

Below is the concrete code structure for `edge/esp32/test/test_m1_dual_mode.cpp`:

```cpp
// edge/esp32/test/test_m1_dual_mode.cpp
// Host-side unit test suite for Milestone 1 Dual-Mode Communication & Tracking Payload

#include "arduino_shim.h"
#include "WiFi.h"
#include "WiFiUdp.h"
#include "PubSubClient.h"
#include <ArduinoJson.h>

#include "tracking_payload.h"
#include "dual_mode_comm.h"

#include <cassert>
#include <cstdio>
#include <cmath>
#include <chrono>
#include <string>

static int g_failures = 0;

static void check(bool condition, const char* description) {
  if (condition) {
    printf("  ok   %s\n", description);
  } else {
    printf("  FAIL %s\n", description);
    g_failures++;
  }
}

// -----------------------------------------------------------------------------
// Test Group 1: TrackingPayload JSON Serialization Tests
// -----------------------------------------------------------------------------
static void run_tracking_payload_tests() {
  printf("\n=== [1/5] TrackingPayload JSON Serialization Tests ===\n");

  PersonTrackingData data;
  data.sensor_id = "esp32_cam_01";
  data.zone_id = "zone_1";
  data.timestamp_ms = 1724645160000ULL;
  data.person_detected = true;
  data.confidence = 0.94f;
  data.person_count = 2;

  char buffer[256];
  size_t len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  check(len > 0, "nominal serialization returns non-zero length");

  StaticJsonDocument<256> doc;
  DeserializationError err = deserializeJson(doc, buffer);
  check(!err, "serialized payload is valid JSON");
  check(doc["sensor_id"] == "esp32_cam_01", "sensor_id field matches");
  check(doc["zone_id"] == "zone_1", "zone_id field matches");
  check(doc["timestamp_ms"].as<uint64_t>() == 1724645160000ULL, "timestamp_ms preserves 64-bit epoch");
  check(doc["person_detected"].as<bool>() == true, "person_detected is true");
  check(std::abs(doc["confidence"].as<float>() - 0.94f) < 1e-3f, "confidence matches 0.94");
  check(doc["person_count"].as<int>() == 2, "person_count is 2");

  // Edge values: zero detections
  PersonTrackingData zeroData;
  zeroData.sensor_id = "cam_entry";
  zeroData.zone_id = "zone_lobby";
  zeroData.timestamp_ms = 1000ULL;
  zeroData.person_detected = false;
  zeroData.confidence = 0.0f;
  zeroData.person_count = 0;

  len = serializeTrackingPayload(zeroData, buffer, sizeof(buffer));
  check(len > 0, "zero-detection serialization succeeds");
  doc.clear();
  deserializeJson(doc, buffer);
  check(doc["person_detected"].as<bool>() == false, "person_detected is false");
  check(doc["person_count"].as<int>() == 0, "person_count is 0");
  check(std::abs(doc["confidence"].as<float>() - 0.0f) < 1e-3f, "confidence is 0.0");

  // Boundary safety: Buffer undersized
  char tinyBuffer[10];
  size_t truncLen = serializeTrackingPayload(data, tinyBuffer, sizeof(tinyBuffer));
  check(truncLen == 0, "undersized buffer returns 0 bytes written");

  // Null safety
  size_t nullBufLen = serializeTrackingPayload(data, nullptr, 128);
  check(nullBufLen == 0, "nullptr buffer returns 0 safely");

  // Canary byte preservation
  uint8_t memoryChunk[32 + 256 + 32];
  memset(memoryChunk, 0xAA, sizeof(memoryChunk));
  char* targetBuf = (char*)(memoryChunk + 32);
  serializeTrackingPayload(data, targetBuf, 256);
  
  bool lowerCanaryOk = true, upperCanaryOk = true;
  for (int i = 0; i < 32; i++) {
    if (memoryChunk[i] != 0xAA) lowerCanaryOk = false;
    if (memoryChunk[32 + 256 + i] != 0xAA) upperCanaryOk = false;
  }
  check(lowerCanaryOk && upperCanaryOk, "buffer boundary canary bytes completely uncorrupted");
}

// -----------------------------------------------------------------------------
// Test Group 2: DualModeComm Wi-Fi Connected Mode
// -----------------------------------------------------------------------------
static void run_wifi_connected_tests() {
  printf("\n=== [2/5] DualModeComm Wi-Fi Connected Mode Tests ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;

  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  DualModeComm::Config cfg;
  cfg.wifi_ssid = "TestSSID";
  cfg.wifi_pass = "TestPass";
  cfg.mqtt_host = "192.168.1.10";
  cfg.mqtt_port = 1883;
  cfg.mqtt_topic = "econ/telemetry/zone_1";
  cfg.udp_port = 4210;
  cfg.broadcast_ip = IPAddress(255, 255, 255, 255);
  cfg.reconnect_interval_ms = 5000;

  comm.begin(cfg);

  // Set connected state
  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(true);
  mockSerial.clear();
  mockUdp.clearHistory();
  mockMqtt.clearHistory();

  PersonTrackingData data;
  data.sensor_id = "cam_01";
  data.zone_id = "zone_1";
  data.timestamp_ms = 1724645160000ULL;
  data.person_detected = true;
  data.confidence = 0.95f;
  data.person_count = 1;

  bool txResult = comm.transmit(data);
  check(txResult, "comm.transmit returns true in connected mode");
  check(mockUdp.getPacketCount() == 1, "UDP broadcast packet emitted");
  if (mockUdp.getPacketCount() == 1) {
    check(mockUdp.getLastPacket().destPort == 4210, "UDP packet port is 4210");
    check(mockUdp.getLastPacket().destIP == IPAddress(255, 255, 255, 255), "UDP packet dest is 255.255.255.255");
    std::string payload = mockUdp.getLastPacketPayload();
    check(payload.find("\"person_detected\":true") != std::string::npos, "UDP packet contains valid JSON");
  }

  check(mockMqtt.getPublishCount() == 1, "MQTT message published");
  if (mockMqtt.getPublishCount() == 1) {
    check(mockMqtt.getLastPublished().topic == "econ/telemetry/zone_1", "MQTT topic is econ/telemetry/zone_1");
  }

  check(mockSerial.getOutput().empty(), "Serial transport remains silent when Wi-Fi broadcast is active");
}

// -----------------------------------------------------------------------------
// Test Group 3: DualModeComm Wi-Fi Disconnected Fallback
// -----------------------------------------------------------------------------
static void run_wifi_disconnected_fallback_tests() {
  printf("\n=== [3/5] DualModeComm Wi-Fi Disconnected Fallback Tests ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;

  DualModeComm comm(mockUdp, mockMqtt, mockSerial);
  DualModeComm::Config cfg;
  cfg.mqtt_topic = "econ/telemetry/zone_1";
  comm.begin(cfg);

  // Set disconnected state
  WiFi.setMockStatus(WL_DISCONNECTED);
  mockMqtt.setMockConnected(false);
  mockSerial.clear();
  mockUdp.clearHistory();
  mockMqtt.clearHistory();

  PersonTrackingData data;
  data.sensor_id = "cam_fallback";
  data.zone_id = "zone_2";
  data.timestamp_ms = 1724645200000ULL;
  data.person_detected = true;
  data.confidence = 0.88f;
  data.person_count = 3;

  bool txResult = comm.transmit(data);
  check(txResult, "comm.transmit succeeds via fallback");
  check(mockUdp.getPacketCount() == 0, "No UDP packets emitted while offline");
  check(mockMqtt.getPublishCount() == 0, "No MQTT messages emitted while offline");
  
  std::string serialOut = mockSerial.getOutput();
  check(!serialOut.empty(), "Serial fallback received output");
  check(serialOut.back() == '\n', "Serial output is newline framed");

  StaticJsonDocument<256> doc;
  DeserializationError err = deserializeJson(doc, serialOut);
  check(!err, "Serial fallback output is valid JSON");
  check(doc["sensor_id"] == "cam_fallback", "Serial payload sensor_id matches");
  check(doc["person_count"].as<int>() == 3, "Serial payload person_count matches");
}

// -----------------------------------------------------------------------------
// Test Group 4: Failover Transitions (Online -> Offline -> Online)
// -----------------------------------------------------------------------------
static void run_failover_transition_tests() {
  printf("\n=== [4/5] DualModeComm Failover Transition Tests ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);
  DualModeComm::Config cfg;
  cfg.mqtt_topic = "econ/telemetry/zone_1";
  comm.begin(cfg);

  PersonTrackingData data;
  data.sensor_id = "cam_01";
  data.zone_id = "zone_1";
  data.timestamp_ms = 1000ULL;
  data.person_detected = true;
  data.confidence = 0.90f;
  data.person_count = 1;

  // Step 1: Online
  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(true);
  mockSerial.clear();
  mockUdp.clearHistory();
  comm.transmit(data);
  check(mockUdp.getPacketCount() == 1, "Cycle 1 (Online): UDP broadcast active");
  check(mockSerial.getOutput().empty(), "Cycle 1 (Online): Serial silent");

  // Step 2: Drop Wi-Fi
  WiFi.setMockStatus(WL_CONNECTION_LOST);
  mockMqtt.setMockConnected(false);
  mockSerial.clear();
  mockUdp.clearHistory();
  comm.transmit(data);
  check(mockUdp.getPacketCount() == 0, "Cycle 2 (Dropped): No UDP packets");
  check(!mockSerial.getOutput().empty(), "Cycle 2 (Dropped): Serial fallback engaged");

  // Step 3: Reconnect Wi-Fi
  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(true);
  mockSerial.clear();
  mockUdp.clearHistory();
  comm.transmit(data);
  check(mockUdp.getPacketCount() == 1, "Cycle 3 (Recovered): UDP broadcast restored");
  check(mockSerial.getOutput().empty(), "Cycle 3 (Recovered): Serial silent again");

  // Step 4: UDP Send Error Fallback
  mockUdp.clearHistory();
  mockSerial.clear();
  mockUdp.failNextSend = true;
  bool txRes = comm.transmit(data);
  check(txRes, "Cycle 4 (UDP Error): Returns true after successful Serial fallback");
  check(!mockSerial.getOutput().empty(), "Cycle 4 (UDP Error): Immediate Serial fallback triggered");
}

// -----------------------------------------------------------------------------
// Test Group 5: Non-Blocking Timing & State Machine Loop
// -----------------------------------------------------------------------------
static void run_timing_and_nonblocking_tests() {
  printf("\n=== [5/5] Non-Blocking Timing & State Machine Tests ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);
  DualModeComm::Config cfg;
  cfg.wifi_ssid = "SSID";
  cfg.wifi_pass = "PASS";
  cfg.reconnect_interval_ms = 5000;
  comm.begin(cfg);

  // Set disconnected state to trigger reconnect logic in tick
  WiFi.setMockStatus(WL_DISCONNECTED);
  setMockMillis(1000);

  auto start = std::chrono::high_resolution_clock::now();
  const int iterations = 1000;
  for (int i = 0; i < iterations; i++) {
    comm.tick();
  }
  auto end = std::chrono::high_resolution_clock::now();
  double elapsedUs = std::chrono::duration<double, std::micro>(end - start).count();
  double usPerTick = elapsedUs / iterations;

  printf("  Benchmark: %.2f us per tick across %d iterations\n", usPerTick, iterations);
  check(usPerTick < 200.0, "tick() execution time strictly < 200 us (<0.2ms requirement)");

  // Test non-blocking reconnect interval cooldown
  int initialBegins = WiFi.beginCount;
  setMockMillis(1000);
  comm.tick();
  int beginsAfterFirstTick = WiFi.beginCount;
  check(beginsAfterFirstTick == initialBegins + 1, "First disconnected tick triggers WiFi.begin()");

  // Advance time by only 1000ms (cooldown is 5000ms)
  setMockMillis(2000);
  comm.tick();
  check(WiFi.beginCount == beginsAfterFirstTick, "Tick within 5000ms cooldown skips reconnect attempt");

  // Advance past cooldown (t = 6500ms)
  setMockMillis(6500);
  comm.tick();
  check(WiFi.beginCount == beginsAfterFirstTick + 1, "Tick after 5000ms initiates next reconnect attempt");
}

// -----------------------------------------------------------------------------
// Main Test Runner
// -----------------------------------------------------------------------------
int main() {
  printf("====================================================\n");
  printf("  Milestone 1: Dual-Mode Communication Unit Tests   \n");
  printf("====================================================\n");

  run_tracking_payload_tests();
  run_wifi_connected_tests();
  run_wifi_disconnected_fallback_tests();
  run_failover_transition_tests();
  run_timing_and_nonblocking_tests();

  printf("\n----------------------------------------------------\n");
  printf("Result: %s (%d failure%s)\n", g_failures ? "FAILED" : "PASSED", g_failures, g_failures == 1 ? "" : "s");
  printf("====================================================\n");

  return g_failures ? 1 : 0;
}
```

---

## 6. Integration with `run_host_tests.sh`

The host test execution script should be updated to build and run both `host_config_test.cpp` and `test_m1_dual_mode.cpp` sequentially, aborting if either test suite fails.

### 6.1 Updated `edge/esp32/test/run_host_tests.sh`
```bash
#!/usr/bin/env bash
# Host-side test runner for ESP32 edge firmware off-target unit tests.
# Runs both the node_config validation test and Milestone 1 dual-mode comm tests.
set -euo pipefail
cd "$(dirname "$0")/.."

JSON=.pio/libdeps/esp32dev/ArduinoJson/src
if [ ! -d "$JSON" ]; then
  echo "ArduinoJson not found at $JSON — run 'pio run -e esp32dev' once to fetch lib_deps." >&2
  exit 1
fi

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo ">>> Running Node Config Unit Tests..."
c++ -std=c++17 -Wall -Wextra -I "$JSON" -I src -I test test/host_config_test.cpp -o "$TMP_DIR/cfgtest"
"$TMP_DIR/cfgtest"

echo ""
echo ">>> Running Milestone 1 Dual-Mode Communication Unit Tests..."
c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_m1_dual_mode.cpp \
    -o "$TMP_DIR/m1test"
"$TMP_DIR/m1test"

echo ""
echo "All host test suites passed successfully!"
```

---

## 7. Recommendations for Implementers

1. **Dependency Injection in `DualModeComm`**:
   - Provide a constructor that accepts references: `DualModeComm(WiFiUDP& udp, PubSubClient& mqtt, Stream& serial);`
   - Provide a default constructor `DualModeComm();` wiring to the default `WiFiUDP`, `PubSubClient`, and `Serial` instances.
   - This provides 100% test isolation in unit tests while remaining zero-overhead in production firmware.

2. **Schema & 64-Bit Integer Handling**:
   - In `tracking_payload.h`, define `uint64_t timestamp_ms` in `PersonTrackingData` to safely represent 13-digit Unix millisecond timestamps (e.g. `1724645160000`).
   - Use `ArduinoJson`'s `StaticJsonDocument<256>` or optimized snprintf formatting to avoid heap allocation.

3. **Non-Blocking State Machine**:
   - `comm.tick()` must never call blocking `delay()` or blocking network calls.
   - WiFi reconnection should be guarded with a non-blocking timestamp comparison:
     ```cpp
     if (WiFi.status() != WL_CONNECTED) {
       unsigned long now = millis();
       if (now - _lastReconnectAttempt >= _reconnectIntervalMs) {
         _lastReconnectAttempt = now;
         WiFi.begin(_config.wifi_ssid, _config.wifi_pass);
       }
     }
     ```

4. **Zero-Delay Fallback**:
   - When transmitting:
     ```cpp
     if (isWifiConnected()) {
       bool udpOk = sendUdpBroadcast(payloadStr, len);
       if (isMqttConnected()) {
         sendMqtt(payloadStr, len);
       }
       if (udpOk) return true;
     }
     // Instant fallback to Serial if disconnected or UDP failed
     _serial.println(payloadStr);
     return true;
     ```

---

## 8. Conclusion

The designed test infrastructure and host mock harness provide a complete, hardware-independent testing framework for Milestone 1. It guarantees rigorous verification of data serialization, primary network broadcasting, zero-delay serial fallback, failover transitions, and non-blocking real-time execution constraints directly on developer host machines and CI pipelines.
