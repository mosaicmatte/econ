// -----------------------------------------------------------------------------
// arduino_shim.h — Comprehensive Host Shims for ESP32/Arduino Off-Target Testing
//
// Provides high-fidelity mocks and shims for:
// - Serial output capture, formatted printing, and UART simulation
// - Preferences NVS key-value and blob storage
// - WiFi stack and state simulation (WL_CONNECTED, WL_DISCONNECTED, RSSI, IP)
// - WiFiUDP broadcast and unicast socket mocks with packet capture
// - Arduino timing (millis, micros, delay, simulated clock progression)
// - GPIO and hardware simulation stubs
// - Comprehensive opaque-box test assertions and reporting framework
// -----------------------------------------------------------------------------
#pragma once

#include <cstdarg>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cmath>
#include <string>
#include <vector>
#include <map>
#include <sstream>
#include <iomanip>
#include <chrono>
#include <functional>
#include <algorithm>
#include <iostream>

// =============================================================================
// 1. Arduino Types & Constants
// =============================================================================
#ifndef HIGH
#define HIGH 0x1
#endif
#ifndef LOW
#define LOW  0x0
#endif
#ifndef INPUT
#define INPUT 0x0
#endif
#ifndef OUTPUT
#define OUTPUT 0x1
#endif
#ifndef INPUT_PULLUP
#define INPUT_PULLUP 0x2
#endif

#ifndef PROGMEM
#define PROGMEM
#endif

#ifndef PSTR
#define PSTR(s) (s)
#endif

#ifndef pgm_read_byte
#define pgm_read_byte(addr) (*(const uint8_t*)(addr))
#endif

#ifndef pgm_read_word
#define pgm_read_word(addr) (*(const uint16_t*)(addr))
#endif

#ifndef pgm_read_dword
#define pgm_read_dword(addr) (*(const uint32_t*)(addr))
#endif

#ifndef pgm_read_float
#define pgm_read_float(addr) (*(const float*)(addr))
#endif

#ifndef F
#define F(s) (s)
#endif

// =============================================================================
// 2. Timing Simulation & System Clocks
// =============================================================================
namespace ArduinoShimInternal {
  inline bool& useSimulatedTime() {
    static bool sim = false;
    return sim;
  }
  inline uint32_t& simMillis() {
    static uint32_t m = 0;
    return m;
  }
  inline uint64_t& simMicros() {
    static uint64_t u = 0;
    return u;
  }
  inline std::chrono::steady_clock::time_point& hostStartTime() {
    static auto start = std::chrono::steady_clock::now();
    return start;
  }
}

inline void setSimulatedTime(bool enable, uint32_t start_ms = 0) {
  ArduinoShimInternal::useSimulatedTime() = enable;
  ArduinoShimInternal::simMillis() = start_ms;
  ArduinoShimInternal::simMicros() = (uint64_t)start_ms * 1000ULL;
}

inline void advanceSimulatedTime(uint32_t delta_ms) {
  ArduinoShimInternal::useSimulatedTime() = true;
  ArduinoShimInternal::simMillis() += delta_ms;
  ArduinoShimInternal::simMicros() += (uint64_t)delta_ms * 1000ULL;
}

inline void setSimulatedMillis(uint32_t ms) {
  ArduinoShimInternal::useSimulatedTime() = true;
  ArduinoShimInternal::simMillis() = ms;
  ArduinoShimInternal::simMicros() = (uint64_t)ms * 1000ULL;
}

inline uint32_t millis() {
  if (ArduinoShimInternal::useSimulatedTime()) {
    return ArduinoShimInternal::simMillis();
  }
  auto now = std::chrono::steady_clock::now();
  return (uint32_t)std::chrono::duration_cast<std::chrono::milliseconds>(now - ArduinoShimInternal::hostStartTime()).count();
}

inline uint32_t micros() {
  if (ArduinoShimInternal::useSimulatedTime()) {
    return (uint32_t)ArduinoShimInternal::simMicros();
  }
  auto now = std::chrono::steady_clock::now();
  return (uint32_t)std::chrono::duration_cast<std::chrono::microseconds>(now - ArduinoShimInternal::hostStartTime()).count();
}

inline void delay(uint32_t ms) {
  if (ArduinoShimInternal::useSimulatedTime()) {
    ArduinoShimInternal::simMillis() += ms;
    ArduinoShimInternal::simMicros() += (uint64_t)ms * 1000ULL;
  } else {
    // In fast host tests, simulate brief delay without sleeping actual wall clock
    ArduinoShimInternal::simMillis() += ms;
  }
}

inline void delayMicroseconds(uint32_t us) {
  if (ArduinoShimInternal::useSimulatedTime()) {
    ArduinoShimInternal::simMicros() += us;
    ArduinoShimInternal::simMillis() = (uint32_t)(ArduinoShimInternal::simMicros() / 1000ULL);
  }
}

inline void yield() {}

// =============================================================================
// 3. GPIO & Hardware Stubs
// =============================================================================
inline void pinMode(uint8_t, uint8_t) {}
inline void digitalWrite(uint8_t, uint8_t) {}
inline int digitalRead(uint8_t) { return LOW; }
inline int analogRead(uint8_t) { return 0; }
inline void analogWrite(uint8_t, int) {}

// =============================================================================
// 4. IPAddress Mock
// =============================================================================
class IPAddress {
public:
  uint8_t _bytes[4] = {0, 0, 0, 0};

  IPAddress() {
    _bytes[0] = _bytes[1] = _bytes[2] = _bytes[3] = 0;
  }
  IPAddress(uint8_t b0, uint8_t b1, uint8_t b2, uint8_t b3) {
    _bytes[0] = b0; _bytes[1] = b1; _bytes[2] = b2; _bytes[3] = b3;
  }
  IPAddress(uint32_t addr) {
    _bytes[0] = (uint8_t)(addr & 0xFF);
    _bytes[1] = (uint8_t)((addr >> 8) & 0xFF);
    _bytes[2] = (uint8_t)((addr >> 16) & 0xFF);
    _bytes[3] = (uint8_t)((addr >> 24) & 0xFF);
  }
  IPAddress(const char* str) {
    fromString(str);
  }

  bool fromString(const char* str) {
    if (!str) return false;
    int b[4];
    if (sscanf(str, "%d.%d.%d.%d", &b[0], &b[1], &b[2], &b[3]) == 4) {
      for (int i = 0; i < 4; ++i) {
        if (b[i] < 0 || b[i] > 255) return false;
        _bytes[i] = (uint8_t)b[i];
      }
      return true;
    }
    return false;
  }

  uint8_t operator[](int index) const {
    if (index >= 0 && index < 4) return _bytes[index];
    return 0;
  }
  uint8_t& operator[](int index) {
    return _bytes[index];
  }

  bool operator==(const IPAddress& other) const {
    return memcmp(_bytes, other._bytes, 4) == 0;
  }
  bool operator!=(const IPAddress& other) const {
    return !(*this == other);
  }

  std::string toString() const {
    char buf[20];
    snprintf(buf, sizeof(buf), "%d.%d.%d.%d", _bytes[0], _bytes[1], _bytes[2], _bytes[3]);
    return std::string(buf);
  }
};

inline std::ostream& operator<<(std::ostream& os, const IPAddress& ip) {
  return os << ip.toString();
}

// =============================================================================
// 5. Serial Output & Input Capture Mock
// =============================================================================
#ifndef WIFI_STA
#define WIFI_STA 1
#endif
#ifndef WIFI_AP
#define WIFI_AP 2
#endif
#ifndef WIFI_OFF
#define WIFI_OFF 0
#endif

class Print {
public:
  virtual ~Print() {}
  virtual size_t write(uint8_t b) = 0;
  virtual size_t write(const uint8_t* buffer, size_t size) {
    if (!buffer || size == 0) return 0;
    size_t n = 0;
    while (size--) {
      if (write(*buffer++)) n++;
      else break;
    }
    return n;
  }
};

class Stream : public Print {
public:
  virtual ~Stream() {}
  virtual int available() = 0;
  virtual int read() = 0;
  virtual int peek() = 0;
  virtual void flush() = 0;
};

struct SerialShim : public Stream {
  bool capture_enabled = false;
  bool echo_to_stdout = true;
  std::string captured_buffer;
  std::string input_buffer;
  size_t input_cursor = 0;
  uint32_t baud_rate = 115200;

  void begin(uint32_t baud = 115200) {
    baud_rate = baud;
  }

  void end() {}

  void setCapture(bool enable, bool echo = false) {
    capture_enabled = enable;
    echo_to_stdout = echo;
  }

  void clearCapture() {
    captured_buffer.clear();
  }

  void clear() {
    captured_buffer.clear();
  }

  const std::string& getCaptured() const {
    return captured_buffer;
  }

  const std::string& getOutput() const {
    return captured_buffer;
  }

  void setInput(const std::string& input) {
    input_buffer = input;
    input_cursor = 0;
  }

  int available() override {
    return (int)(input_buffer.size() - input_cursor);
  }

  int read() override {
    if (input_cursor < input_buffer.size()) {
      return (uint8_t)input_buffer[input_cursor++];
    }
    return -1;
  }

  int peek() override {
    if (input_cursor < input_buffer.size()) {
      return (uint8_t)input_buffer[input_cursor];
    }
    return -1;
  }

  void flush() override {}

  size_t write(uint8_t b) override {
    char c = (char)b;
    if (capture_enabled) {
      captured_buffer.push_back(c);
    }
    if (echo_to_stdout) {
      putchar(c);
    }
    return 1;
  }

  size_t write(const uint8_t* buffer, size_t size) override {
    if (!buffer || size == 0) return 0;
    if (capture_enabled) {
      captured_buffer.append((const char*)buffer, size);
    }
    if (echo_to_stdout) {
      fwrite(buffer, 1, size, stdout);
    }
    return size;
  }

  size_t write(const char* str) {
    if (!str) return 0;
    return write((const uint8_t*)str, strlen(str));
  }

  void printf(const char* fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    char buf[1024];
    int n = vsnprintf(buf, sizeof(buf), fmt, ap);
    va_end(ap);
    if (n > 0) {
      if (capture_enabled) {
        captured_buffer.append(buf, n);
      }
      if (echo_to_stdout) {
        fputs(buf, stdout);
      }
    }
  }

  void print(const char* s) {
    if (!s) return;
    if (capture_enabled) captured_buffer.append(s);
    if (echo_to_stdout) ::printf("%s", s);
  }
  void print(const std::string& s) { print(s.c_str()); }
  void print(int val) { char b[32]; snprintf(b, sizeof(b), "%d", val); print(b); }
  void print(unsigned int val) { char b[32]; snprintf(b, sizeof(b), "%u", val); print(b); }
  void print(long val) { char b[32]; snprintf(b, sizeof(b), "%ld", val); print(b); }
  void print(unsigned long val) { char b[32]; snprintf(b, sizeof(b), "%lu", val); print(b); }
  void print(float val, int prec = 2) {
    char b[32];
    char fmt[16];
    snprintf(fmt, sizeof(fmt), "%%.%df", prec);
    snprintf(b, sizeof(b), fmt, val);
    print(b);
  }
  void print(double val, int prec = 2) { print((float)val, prec); }
  void print(char c) {
    if (capture_enabled) captured_buffer.push_back(c);
    if (echo_to_stdout) putchar(c);
  }

  void println(const char* s = "") {
    if (!s) s = "";
    if (capture_enabled) {
      captured_buffer.append(s);
      captured_buffer.push_back('\n');
    }
    if (echo_to_stdout) ::printf("%s\n", s);
  }
  void println(const std::string& s) { println(s.c_str()); }
  void println(int val) { print(val); println(); }
  void println(unsigned int val) { print(val); println(); }
  void println(long val) { print(val); println(); }
  void println(unsigned long val) { print(val); println(); }
  void println(float val, int prec = 2) { print(val, prec); println(); }
  void println(double val, int prec = 2) { print(val, prec); println(); }
  void println(char c) { print(c); println(); }
};

inline SerialShim& arduinoShimGetSerial() {
  static SerialShim s;
  return s;
}
#define Serial (arduinoShimGetSerial())

// =============================================================================
// 6. Preferences NVS Key-Value Store Mock
// =============================================================================
// Preserves backward compatibility with host_config_test.cpp PrefStore
struct PrefStore {
  bool has = false;
  std::vector<uint8_t> blob;
  std::map<std::string, std::vector<uint8_t>> entries;
};

inline PrefStore& prefStore() {
  static PrefStore s;
  return s;
}

class Preferences {
private:
  std::string current_ns;
  bool is_readonly = false;

public:
  bool begin(const char* name, bool readonly = false) {
    current_ns = name ? name : "econ";
    is_readonly = readonly;
    return true;
  }

  void end() {}

  size_t putBytes(const char* key, const void* p, size_t n) {
    if (is_readonly || !key || !p) return 0;
    auto& s = prefStore();
    std::string full_key = current_ns + "/" + key;
    std::vector<uint8_t> vec((const uint8_t*)p, (const uint8_t*)p + n);
    s.entries[full_key] = vec;
    // Legacy support for single-blob store
    s.blob = vec;
    s.has = true;
    return n;
  }

  size_t getBytesLength(const char* key) {
    auto& s = prefStore();
    if (!key) return s.has ? s.blob.size() : 0;
    std::string full_key = current_ns + "/" + key;
    auto it = s.entries.find(full_key);
    if (it != s.entries.end()) {
      return it->second.size();
    }
    return s.has ? s.blob.size() : 0;
  }

  size_t getBytes(const char* key, void* out, size_t n) {
    auto& s = prefStore();
    if (!key || !out) return 0;
    std::string full_key = current_ns + "/" + key;
    auto it = s.entries.find(full_key);
    if (it != s.entries.end()) {
      if (it->second.size() != n) return 0;
      memcpy(out, it->second.data(), n);
      return n;
    }
    if (!s.has || s.blob.size() != n) return 0;
    memcpy(out, s.blob.data(), n);
    return n;
  }

  size_t putString(const char* key, const char* value) {
    if (!value) return 0;
    return putBytes(key, value, strlen(value) + 1);
  }

  std::string getString(const char* key, const char* defaultValue = "") {
    size_t len = getBytesLength(key);
    if (len == 0) return defaultValue ? defaultValue : "";
    std::vector<char> buf(len);
    getBytes(key, buf.data(), len);
    return std::string(buf.data());
  }

  size_t putUInt(const char* key, uint32_t value) {
    return putBytes(key, &value, sizeof(value));
  }

  uint32_t getUInt(const char* key, uint32_t defaultValue = 0) {
    uint32_t val = defaultValue;
    if (getBytes(key, &val, sizeof(val)) == sizeof(val)) {
      return val;
    }
    return defaultValue;
  }

  size_t putInt(const char* key, int32_t value) {
    return putBytes(key, &value, sizeof(value));
  }

  int32_t getInt(const char* key, int32_t defaultValue = 0) {
    int32_t val = defaultValue;
    if (getBytes(key, &val, sizeof(val)) == sizeof(val)) {
      return val;
    }
    return defaultValue;
  }

  size_t putFloat(const char* key, float value) {
    return putBytes(key, &value, sizeof(value));
  }

  float getFloat(const char* key, float defaultValue = 0.0f) {
    float val = defaultValue;
    if (getBytes(key, &val, sizeof(val)) == sizeof(val)) {
      return val;
    }
    return defaultValue;
  }

  size_t putBool(const char* key, bool value) {
    uint8_t b = value ? 1 : 0;
    return putBytes(key, &b, sizeof(b));
  }

  bool getBool(const char* key, bool defaultValue = false) {
    uint8_t b = 0;
    if (getBytes(key, &b, sizeof(b)) == sizeof(b)) {
      return b != 0;
    }
    return defaultValue;
  }

  bool remove(const char* key) {
    auto& s = prefStore();
    if (key) {
      std::string full_key = current_ns + "/" + key;
      s.entries.erase(full_key);
    }
    s.has = false;
    s.blob.clear();
    return true;
  }

  bool clear() {
    auto& s = prefStore();
    s.entries.clear();
    s.blob.clear();
    s.has = false;
    return true;
  }
};

// =============================================================================
// 7. WiFi Stack Mock
// =============================================================================
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

class WiFiClass {
public:
  wl_status_t _status = WL_DISCONNECTED;
  int _rssi = -65;
  IPAddress _ip = IPAddress(192, 168, 1, 150);
  IPAddress _subnet = IPAddress(255, 255, 255, 0);
  IPAddress _gateway = IPAddress(192, 168, 1, 1);
  std::string _ssid = "Simulated_AP";
  std::string _mac = "30:AE:A4:1A:2B:3C";
  bool _auto_reconnect = true;
  int _reconnect_attempts = 0;
  int beginCount = 0;
  int disconnectCount = 0;

  wl_status_t status() const {
    return _status;
  }

  bool isConnected() const {
    return _status == WL_CONNECTED;
  }

  void setConnected(bool connected) {
    _status = connected ? WL_CONNECTED : WL_DISCONNECTED;
  }

  void setStatus(wl_status_t s) {
    _status = s;
  }

  void setMockStatus(wl_status_t s) {
    _status = s;
  }

  void setMockIP(const IPAddress& ip) {
    _ip = ip;
  }

  void mode(int) {}

  void setAutoReconnect(bool autoReconnect) {
    _auto_reconnect = autoReconnect;
  }

  bool reconnect() {
    _reconnect_attempts++;
    _status = WL_CONNECTED;
    return true;
  }

  bool disconnect(bool wifioff = false, bool eraseap = false) {
    (void)wifioff; (void)eraseap;
    _status = WL_DISCONNECTED;
    disconnectCount++;
    return true;
  }

  wl_status_t begin(const char* ssid = nullptr, const char* passphrase = nullptr, int32_t channel = 0,
                    const uint8_t* bssid = nullptr, bool connect = true) {
    (void)passphrase; (void)channel; (void)bssid; (void)connect;
    beginCount++;
    if (ssid && strlen(ssid) > 0) {
      _ssid = ssid;
    } else {
      _status = WL_CONNECT_FAILED;
    }
    return _status;
  }

  IPAddress localIP() const {
    return _status == WL_CONNECTED ? _ip : IPAddress(0, 0, 0, 0);
  }

  IPAddress subnetMask() const {
    return _subnet;
  }

  IPAddress gatewayIP() const {
    return _gateway;
  }

  std::string SSID() const {
    return _ssid;
  }

  std::string macAddress() const {
    return _mac;
  }

  int32_t RSSI() const {
    return _status == WL_CONNECTED ? _rssi : 0;
  }

  void setRSSI(int rssi) {
    _rssi = rssi;
  }

  void reset() {
    _status = WL_DISCONNECTED;
    _rssi = -65;
    _ip = IPAddress(192, 168, 1, 150);
    _subnet = IPAddress(255, 255, 255, 0);
    _gateway = IPAddress(192, 168, 1, 1);
    _ssid = "Simulated_AP";
    _auto_reconnect = true;
    _reconnect_attempts = 0;
    beginCount = 0;
    disconnectCount = 0;
  }
};

inline WiFiClass& arduinoShimGetWiFi() {
  static WiFiClass w;
  return w;
}
#define WiFi (arduinoShimGetWiFi())

// =============================================================================
// 8. WiFiUDP Broadcast & Unicast Socket Mock
// =============================================================================
struct UDPSentPacket {
  IPAddress destination_ip;
  uint16_t destination_port;
  std::vector<uint8_t> payload;
  uint32_t timestamp_ms;
  bool is_broadcast;

  IPAddress destIP;
  uint16_t destPort;
  std::vector<uint8_t> data;
};

class WiFiUDP {
public:
  uint16_t _local_port = 0;
  bool _bound = false;
  IPAddress _dest_ip;
  uint16_t _dest_port = 0;
  std::vector<uint8_t> _tx_buffer;
  std::vector<uint8_t> _rx_buffer;
  size_t _rx_cursor = 0;
  bool _fail_all_sends = false;
  bool failNextSend = false;

  std::vector<UDPSentPacket> sent_packets;

  uint8_t begin(uint16_t port) {
    _local_port = port;
    _bound = true;
    return 1;
  }

  void stop() {
    _bound = false;
    _tx_buffer.clear();
  }

  int beginPacket(IPAddress ip, uint16_t port) {
    if (_fail_all_sends) return 0;
    _dest_ip = ip;
    _dest_port = port;
    _tx_buffer.clear();
    return 1;
  }

  int beginPacket(const char* host, uint16_t port) {
    if (_fail_all_sends || !host) return 0;
    _dest_ip = IPAddress(host);
    _dest_port = port;
    _tx_buffer.clear();
    return 1;
  }

  size_t write(uint8_t byte) {
    _tx_buffer.push_back(byte);
    return 1;
  }

  size_t write(const uint8_t* buffer, size_t size) {
    if (!buffer || size == 0) return 0;
    _tx_buffer.insert(_tx_buffer.end(), buffer, buffer + size);
    return size;
  }

  int endPacket() {
    if (_fail_all_sends || failNextSend) {
      if (failNextSend) failNextSend = false;
      return 0;
    }
    bool is_bcast = (_dest_ip[3] == 255 || _dest_ip == IPAddress(255, 255, 255, 255));
    UDPSentPacket pkt;
    pkt.destination_ip = pkt.destIP = _dest_ip;
    pkt.destination_port = pkt.destPort = _dest_port;
    pkt.payload = pkt.data = _tx_buffer;
    pkt.timestamp_ms = millis();
    pkt.is_broadcast = is_bcast;
    sent_packets.push_back(pkt);
    return 1;
  }

  int parsePacket() {
    if (_rx_cursor < _rx_buffer.size()) {
      return (int)(_rx_buffer.size() - _rx_cursor);
    }
    return 0;
  }

  int available() {
    return (int)(_rx_buffer.size() - _rx_cursor);
  }

  int read() {
    if (_rx_cursor < _rx_buffer.size()) {
      return _rx_buffer[_rx_cursor++];
    }
    return -1;
  }

  int read(unsigned char* buffer, size_t len) {
    if (!buffer || len == 0) return 0;
    size_t to_read = std::min(len, _rx_buffer.size() - _rx_cursor);
    memcpy(buffer, _rx_buffer.data() + _rx_cursor, to_read);
    _rx_cursor += to_read;
    return (int)to_read;
  }

  void injectRxPacket(const uint8_t* data, size_t len) {
    _rx_buffer.assign(data, data + len);
    _rx_cursor = 0;
  }

  void clearSentPackets() {
    sent_packets.clear();
  }

  void clearHistory() {
    sent_packets.clear();
  }

  size_t getSentCount() const {
    return sent_packets.size();
  }

  size_t getPacketCount() const {
    return sent_packets.size();
  }

  const UDPSentPacket& getLastPacket() const {
    static UDPSentPacket empty;
    if (sent_packets.empty()) return empty;
    return sent_packets.back();
  }

  std::string getLastPayloadAsString() const {
    if (sent_packets.empty()) return "";
    const auto& p = sent_packets.back().payload;
    return std::string((const char*)p.data(), p.size());
  }

  std::string getLastPacketPayload() const {
    return getLastPayloadAsString();
  }

  void setFailOnSend(bool fail) {
    _fail_all_sends = fail;
  }
};

// =============================================================================
// 9. Comprehensive Test Assertions & Opaque-Box Suite Framework
// =============================================================================
struct TestResultItem {
  std::string tier;
  std::string feature;
  std::string test_name;
  bool passed;
  std::string message;
  uint32_t duration_us;
};

class OpaqueBoxTestRegistry {
public:
  std::vector<TestResultItem> results;
  int total_count = 0;
  int pass_count = 0;
  int fail_count = 0;

  static OpaqueBoxTestRegistry& instance() {
    static OpaqueBoxTestRegistry reg;
    return reg;
  }

  void record(const std::string& tier, const std::string& feature, const std::string& test_name,
              bool passed, const std::string& message = "", uint32_t duration_us = 0) {
    total_count++;
    if (passed) pass_count++;
    else fail_count++;

    TestResultItem item;
    item.tier = tier;
    item.feature = feature;
    item.test_name = test_name;
    item.passed = passed;
    item.message = message;
    item.duration_us = duration_us;
    results.push_back(item);

    if (passed) {
      std::cout << "  [PASS] [" << tier << "][" << feature << "] " << test_name << "\n";
    } else {
      std::cout << "  [FAIL] [" << tier << "][" << feature << "] " << test_name
                << " -- " << message << "\n";
    }
  }

  void printSummary() {
    std::cout << "\n================================================================================\n";
    std::cout << "               OPAQUE-BOX E2E TEST SUITE EXECUTION SUMMARY                      \n";
    std::cout << "================================================================================\n";

    std::map<std::string, std::pair<int, int>> tier_stats; // tier -> (pass, total)
    std::map<std::string, std::pair<int, int>> feat_stats; // feat -> (pass, total)

    for (const auto& r : results) {
      tier_stats[r.tier].second++;
      if (r.passed) tier_stats[r.tier].first++;

      feat_stats[r.feature].second++;
      if (r.passed) feat_stats[r.feature].first++;
    }

    std::cout << "\n--- Statistics by Tier ---\n";
    for (const auto& kv : tier_stats) {
      std::cout << "  " << std::left << std::setw(30) << kv.first << ": "
                << kv.second.first << " / " << kv.second.second << " passed ("
                << (kv.second.second > 0 ? (kv.second.first * 100 / kv.second.second) : 0) << "%)\n";
    }

    std::cout << "\n--- Statistics by Feature ---\n";
    for (const auto& kv : feat_stats) {
      std::cout << "  " << std::left << std::setw(35) << kv.first << ": "
                << kv.second.first << " / " << kv.second.second << " passed ("
                << (kv.second.second > 0 ? (kv.second.first * 100 / kv.second.second) : 0) << "%)\n";
    }

    std::cout << "\n--------------------------------------------------------------------------------\n";
    std::cout << "TOTAL TESTS: " << total_count << " | PASSED: " << pass_count
              << " | FAILED: " << fail_count << "\n";
    std::cout << "OVERALL STATUS: " << (fail_count == 0 ? "ALL TESTS PASSED (SUCCESS)" : "FAILED") << "\n";
    std::cout << "================================================================================\n\n";
  }

  bool isAllPassed() const {
    return fail_count == 0 && total_count > 0;
  }
};

#define TEST_RECORD(tier, feat, name, cond, msg) \
  do { \
    bool __c = (cond); \
    ::OpaqueBoxTestRegistry::instance().record(tier, feat, name, __c, __c ? "" : (msg)); \
  } while (0)

#define TEST_ASSERT(cond, msg) \
  do { \
    if (!(cond)) { \
      std::ostringstream __oss; \
      __oss << "Assertion failed: (" #cond ") at " << __FILE__ << ":" << __LINE__ << " -- " << (msg); \
      return {false, __oss.str()}; \
    } \
  } while (0)

#define TEST_ASSERT_EQ(expected, actual, msg) \
  do { \
    if ((expected) != (actual)) { \
      std::ostringstream __oss; \
      __oss << "Expected [" << (expected) << "] but got [" << (actual) << "] at " \
            << __FILE__ << ":" << __LINE__ << " -- " << (msg); \
      return {false, __oss.str()}; \
    } \
  } while (0)

#define TEST_ASSERT_NE(a, b, msg) \
  do { \
    if ((a) == (b)) { \
      std::ostringstream __oss; \
      __oss << "Expected values to differ, both are [" << (a) << "] at " \
            << __FILE__ << ":" << __LINE__ << " -- " << (msg); \
      return {false, __oss.str()}; \
    } \
  } while (0)

#define TEST_ASSERT_FLOAT_NEAR(expected, actual, eps, msg) \
  do { \
    float __diff = std::abs((float)(expected) - (float)(actual)); \
    if (__diff > (float)(eps)) { \
      std::ostringstream __oss; \
      __oss << "Expected ~[" << (expected) << "] but got [" << (actual) \
            << "] (diff " << __diff << " > eps " << (eps) << ") at " \
            << __FILE__ << ":" << __LINE__ << " -- " << (msg); \
      return {false, __oss.str()}; \
    } \
  } while (0)

#define TEST_ASSERT_STR_EQ(expected, actual, msg) \
  do { \
    std::string __exp = (expected); \
    std::string __act = (actual); \
    if (__exp != __act) { \
      std::ostringstream __oss; \
      __oss << "Expected string [\"" << __exp << "\"] but got [\"" << __act << "\"] at " \
            << __FILE__ << ":" << __LINE__ << " -- " << (msg); \
      return {false, __oss.str()}; \
    } \
  } while (0)

#define TEST_ASSERT_STR_CONTAINS(haystack, needle, msg) \
  do { \
    std::string __hay = (haystack); \
    std::string __ned = (needle); \
    if (__hay.find(__ned) == std::string::npos) { \
      std::ostringstream __oss; \
      __oss << "Expected [\"" << __hay << "\"] to contain [\"" << __ned << "\"] at " \
            << __FILE__ << ":" << __LINE__ << " -- " << (msg); \
      return {false, __oss.str()}; \
    } \
  } while (0)

struct TestOutcome {
  bool passed;
  std::string error_msg;
};
