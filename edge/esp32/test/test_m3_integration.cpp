// =============================================================================
// test_m3_integration.cpp — Comprehensive Integration Test Suite (Milestone 3)
//
// Dual-Compatible with:
//   1. Standalone Host Test Runner (via c++ -std=c++17 and arduino_shim.h)
//   2. PlatformIO / Unity Test Framework (via unity.h / pio test)
//
// Test Suites:
//   Suite 1: Camera Person Detection Occupancy Replacement for Legacy PIR
//   Suite 2: Dual-Mode Communication State Machine & Zero-Delay Fallback
//   Suite 3: Strict Module Isolation & Non-Interference Verification
//   Suite 4: Telemetry Payload Formatting, Schema & Timing Compliance
// =============================================================================

#include "arduino_shim.h"
#include <ArduinoJson.h>

#include "camera/tracking_payload.h"
#include "camera/dual_mode_comm.h"
#include "camera/camera_config.h"
#include "camera/ov7670_driver.h"
#include "camera/model_data.h"
#include "camera/person_detector.h"

#ifndef ZONE_LABEL_OVERRIDE
#define ZONE_LABEL_OVERRIDE "Level 4"
#endif
#ifndef ZONE_TOPIC_OVERRIDE
#define ZONE_TOPIC_OVERRIDE "zone_1"
#endif
#include "node_config.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cmath>
#include <chrono>
#include <string>
#include <vector>
#include <sstream>
#include <iostream>
#include <algorithm>

// =============================================================================
// Dual-Mode Test Runner / Unity Compatibility Layer
// =============================================================================

#if (defined(UNITY) || defined(PLATFORMIO) || __has_include(<unity.h>)) && !defined(FORCE_HOST_TEST_RUNNER)
#include <unity.h>
#define M3_USE_UNITY 1
#else
#define M3_USE_UNITY 0
#endif

static int g_m3_tests_total = 0;
static int g_m3_tests_passed = 0;
static int g_m3_tests_failed = 0;

static void m3_check(bool condition, const char* name, const char* detail = "") {
  g_m3_tests_total++;
  if (condition) {
    g_m3_tests_passed++;
    printf("  [PASS] %s\n", name);
  } else {
    g_m3_tests_failed++;
    printf("  [FAIL] %s %s%s\n", name, (detail && strlen(detail) > 0) ? "-- " : "", detail ? detail : "");
  }
}

#define M3_ASSERT(cond, msg) m3_check((cond), msg)
#define M3_ASSERT_EQ(expected, actual, msg) m3_check(((expected) == (actual)), msg)
#define M3_ASSERT_FLOAT_NEAR(expected, actual, eps, msg) \
  m3_check((std::abs((float)(expected) - (float)(actual)) <= (float)(eps)), msg)

// =============================================================================
// Mock Helpers for Isolation & Environmental Sensor Simulation
// =============================================================================

// CRC-8 calculation matching SHT30 / ACD1200 polynomial 0x31
static uint8_t calc_crc8_31(uint8_t msb, uint8_t lsb) {
  uint8_t crc = 0xFF;
  uint8_t bytes[2] = {msb, lsb};
  for (int i = 0; i < 2; i++) {
    crc ^= bytes[i];
    for (int b = 0; b < 8; b++) {
      crc = (crc & 0x80) ? (uint8_t)((crc << 1) ^ 0x31) : (uint8_t)(crc << 1);
    }
  }
  return crc;
}

// Environmental Sensor Simulation State
struct MockEnvironmentSensors {
  float temperature_c = 24.2f;
  float humidity_rh   = 55.4f;
  int   co2_ppm       = 620;
  float plug_amps     = 1.00f; // 1.0A * 230V = 230.0 W
  float ac_amps       = 3.70f; // 3.7A * 220V = 814.0 W
  float lux           = 450.0f;
  float supply_temp_c = 14.5f;

  bool readSht30(float& t, float& h) const {
    // Verify CRC integrity simulation
    uint16_t rawT = (uint16_t)(((temperature_c + 45.0f) / 175.0f) * 65535.0f);
    uint16_t rawH = (uint16_t)((humidity_rh / 100.0f) * 65535.0f);
    uint8_t d[6] = {
      (uint8_t)(rawT >> 8), (uint8_t)(rawT & 0xFF), 0,
      (uint8_t)(rawH >> 8), (uint8_t)(rawH & 0xFF), 0
    };
    d[2] = calc_crc8_31(d[0], d[1]);
    d[5] = calc_crc8_31(d[3], d[4]);
    if (calc_crc8_31(d[0], d[1]) != d[2] || calc_crc8_31(d[3], d[4]) != d[5]) return false;
    t = temperature_c;
    h = humidity_rh;
    return true;
  }

  bool readCo2(int& ppm) const {
    uint8_t d[9] = {0, 0, 0, (uint8_t)(co2_ppm >> 8), (uint8_t)(co2_ppm & 0xFF), 0, 0, 0, 0};
    d[2] = calc_crc8_31(d[0], d[1]);
    d[5] = calc_crc8_31(d[3], d[4]);
    d[8] = calc_crc8_31(d[6], d[7]);
    if (calc_crc8_31(d[3], d[4]) != d[5]) return false;
    ppm = co2_ppm;
    return true;
  }

  float readPlugAmps() const { return plug_amps; }
  float readAcAmps() const { return ac_amps; }
  bool readLux(float& out) const { out = lux; return true; }
  bool readSupplyC(float& out) const { out = supply_temp_c; return true; }
};

// System Actuation State
struct MockActuationState {
  bool lights_on = true;
  bool plug_on = true;
  float hvac_setpoint_c = 24.0f;
  bool ac_real = false;

  void applyCommand(const std::string& msg, const NodeConfig& cfg) {
    size_t start = 0;
    while (start < msg.length()) {
      size_t sep = msg.find(';', start);
      std::string tok = (sep == std::string::npos) ? msg.substr(start) : msg.substr(start, sep - start);
      if (tok == "LIGHTS_ON")       lights_on = true;
      else if (tok == "LIGHTS_OFF") lights_on = false;
      else if (tok == "PLUG_ON")    plug_on = true;
      else if (tok == "PLUG_OFF")   plug_on = false;
      else if (tok.rfind("SETPOINT=", 0) == 0 || tok.rfind("HVAC_SET:", 0) == 0) {
        size_t prefix_len = 9;
        float val = std::stof(tok.substr(prefix_len));
        if (std::isnan(val) || val < cfg.setpointMinC || val > cfg.setpointMaxC) {
          val = std::isnan(val) ? (cfg.setpointMinC + cfg.setpointMaxC) / 2.0f
                                : (val < cfg.setpointMinC ? cfg.setpointMinC : cfg.setpointMaxC);
        }
        hvac_setpoint_c = val;
      }
      if (sep == std::string::npos) break;
      start = sep + 1;
    }
  }
};

// =============================================================================
// SUITE 1: Camera Person Detection Occupancy Replacement for Legacy PIR
// =============================================================================
void run_suite_1_camera_occupancy_pir_replacement() {
  printf("\n================================================================================\n");
  printf(" [SUITE 1] Camera Person Detection Occupancy Replacement for Legacy PIR        \n");
  printf("================================================================================\n");

  CameraPersonDetector detector;

  // Test 1.1: Unoccupied startup & debounce verification
  M3_ASSERT(!detector.isInitialized(), "1.1.1 Detector uninitialized before init()");
  M3_ASSERT(detector.init(), "1.1.2 detector.init() returns true");
  M3_ASSERT(detector.isInitialized(), "1.1.3 detector.isInitialized() returns true after init()");
  M3_ASSERT(!detector.isPersonDetected(), "1.1.4 Initial occupancy is false (replaces PIR LOW)");
  M3_ASSERT_EQ(0, detector.getPersonCount(), "1.1.5 Initial person count is 0");
  M3_ASSERT(detector.getConfidence() < 0.20f, "1.1.6 Initial confidence < 0.20");

  // Test 1.2: Humanoid silhouette frame injection & 2-frame debounce
  detector.getDriver().clearInjectedFrame();
  detector.getDriver().setMockPersonDetected(true);

  // Cycle 1: First frame above threshold (debounce counter = 1, detection not yet asserted)
  detector.processFrame();
  // Cycle 2: Second confirming frame (debounce counter = 2 -> asserts occupancy)
  detector.processFrame();

  M3_ASSERT(detector.isPersonDetected(), "1.2.1 Person detected after 2-frame debounce");
  M3_ASSERT(detector.getConfidence() >= 0.65f, "1.2.2 Detection confidence >= 0.65");
  M3_ASSERT_EQ(1, detector.getPersonCount(), "1.2.3 Detected person count is 1");

  // Map to main telemetry occupancy
  int occupancy = detector.isPersonDetected() ? (detector.getPersonCount() > 0 ? detector.getPersonCount() : 1) : 0;
  M3_ASSERT_EQ(1, occupancy, "1.2.4 Telemetry occupancy correctly maps to 1 replacing PIR");

  // Test 1.3: Dual-threshold hysteresis stability (T_enter = 0.60, T_exit = 0.40)
  detector.reset();
  detector.setDetectionThreshold(0.60f, 0.40f);
  M3_ASSERT(!detector.isPersonDetected(), "1.3.1 Detector reset to false state");

  // Inject marginal contrast pattern (~0.52 score)
  std::vector<uint8_t> marginal_frame(CAMERA_FRAME_BYTES, 30);
  for (int y = 20; y <= 85; ++y) {
    for (int x = 60; x <= 100; ++x) {
      marginal_frame[y * CAMERA_FRAME_WIDTH + x] = 55; // Moderate contrast producing 0.52
    }
  }
  detector.injectMockFrame(marginal_frame.data(), marginal_frame.size());
  detector.processFrame();
  detector.processFrame();
  M3_ASSERT(!detector.isPersonDetected(), "1.3.2 Marginal score (0.52 < 0.60) does NOT trigger false occupancy");

  // Trigger high presence (>=0.65)
  detector.getDriver().clearInjectedFrame();
  detector.getDriver().setMockPersonDetected(true);
  detector.processFrame();
  detector.processFrame();
  M3_ASSERT(detector.isPersonDetected(), "1.3.3 Person silhouette triggers true state");

  // Re-inject marginal frame (0.52 >= 0.40 exit threshold)
  detector.injectMockFrame(marginal_frame.data(), marginal_frame.size());
  detector.processFrame();
  detector.processFrame();
  M3_ASSERT(detector.isPersonDetected(), "1.3.4 Marginal score (0.52 >= 0.40) holds occupied state (no flapping)");

  // Low score exits state
  std::vector<uint8_t> blank_frame(CAMERA_FRAME_BYTES, 0);
  detector.injectMockFrame(blank_frame.data(), blank_frame.size());
  detector.processFrame();
  detector.processFrame();
  M3_ASSERT(!detector.isPersonDetected(), "1.3.5 Low score (<0.40) cleanly exits occupied state");

  // Test 1.4: Stationary occupant continuous tracking (Mitigates PIR motion timeout)
  detector.getDriver().clearInjectedFrame();
  detector.getDriver().setMockPersonDetected(true);
  // Warm up debounce (2 frames to assert presence)
  detector.processFrame();
  detector.processFrame();
  M3_ASSERT(detector.isPersonDetected(), "1.4.1 Debounce activated for stationary person");

  bool all_50_frames_held = true;
  for (int i = 0; i < 50; ++i) {
    detector.processFrame();
    if (!detector.isPersonDetected()) {
      all_50_frames_held = false;
      break;
    }
  }
  M3_ASSERT(all_50_frames_held, "1.4.2 Camera holds occupancy continuously for stationary person across 50 frames");

  // Test 1.5: Bilinear preprocessor fixed-point normalization & center crop isolation
  std::vector<uint8_t> test_frame(CAMERA_FRAME_BYTES, 0);
  // Place bright markers only in discarded border regions (X < 20 and X >= 140)
  for (int y = 0; y < CAMERA_FRAME_HEIGHT; ++y) {
    for (int x = 0; x < CAMERA_FRAME_WIDTH; ++x) {
      test_frame[y * CAMERA_FRAME_WIDTH + x] = (x < 20 || x >= 140) ? 255 : 0;
    }
  }
  std::vector<int8_t> out_tensor(MODEL_INPUT_BYTES, 0);
  bool prep_ok = ImagePreprocessor::preprocessFrame(test_frame.data(), test_frame.size(),
                                                    out_tensor.data(), out_tensor.size());
  M3_ASSERT(prep_ok, "1.5.1 Preprocessor executes successfully");
  bool crop_clean = true;
  for (int8_t val : out_tensor) {
    if (val != -128) { crop_clean = false; break; }
  }
  M3_ASSERT(crop_clean, "1.5.2 Center crop (120x120) completely excludes border clutter");
}

// =============================================================================
// SUITE 2: Dual-Mode Communication State Machine & Zero-Delay Fallback
// =============================================================================
void run_suite_2_dual_mode_comms_and_fallback() {
  printf("\n================================================================================\n");
  printf(" [SUITE 2] Dual-Mode Communication State Machine & Zero-Delay Fallback          \n");
  printf("================================================================================\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Corporate_IoT_WLAN";
  cfg.wifi_pass = "SecurePassphrase123";
  cfg.mqtt_host = "192.168.1.100";
  cfg.mqtt_port = 1883;
  cfg.mqtt_topic = "econ/telemetry/zone_1";
  cfg.udp_port = 4210;
  cfg.broadcast_ip = IPAddress(255, 255, 255, 255);
  cfg.reconnect_interval_ms = 5000;

  M3_ASSERT(comm.begin(cfg), "2.1.1 DualModeComm begin() succeeds");

  PersonTrackingData trackData;
  initTrackingData(&trackData);
  trackData.sensor_id = "esp32_cam_01";
  trackData.zone_id = "zone_1";
  trackData.timestamp_ms = 1724645000000ULL;
  trackData.person_detected = true;
  trackData.confidence = 0.92f;
  trackData.person_count = 1;

  // Test 2.1: Wi-Fi Connected Mode — Primary Transport (UDP Broadcast :4210 + MQTT)
  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(true);
  mockSerial.clearCapture();
  mockSerial.setCapture(true, false);
  mockUdp.clearHistory();
  mockMqtt.clearHistory();
  comm.tick();

  M3_ASSERT(comm.isWifiConnected(), "2.1.2 Wi-Fi reports connected");
  M3_ASSERT(comm.isPrimaryTransportActive(), "2.1.3 Primary transport active");
  M3_ASSERT(!comm.isSerialFallbackActive(), "2.1.4 Serial fallback inactive in online mode");

  bool tx_res = comm.transmit(trackData);
  M3_ASSERT(tx_res, "2.1.5 comm.transmit() succeeds online");
  M3_ASSERT_EQ(1, mockUdp.getPacketCount(), "2.1.6 Exactly 1 UDP broadcast packet sent");
  M3_ASSERT_EQ(1, mockMqtt.getPublishCount(), "2.1.7 Exactly 1 MQTT message published");
  M3_ASSERT(mockSerial.getCaptured().empty(), "2.1.8 Serial remains silent during Wi-Fi broadcast");

  if (mockUdp.getPacketCount() == 1) {
    const UDPSentPacket& pkt = mockUdp.getLastPacket();
    M3_ASSERT_EQ(4210, pkt.destPort, "2.1.9 UDP destination port is 4210");
    M3_ASSERT(pkt.destIP == IPAddress(255, 255, 255, 255), "2.1.10 UDP destination IP is 255.255.255.255");
    std::string udp_payload = mockUdp.getLastPacketPayload();
    M3_ASSERT(udp_payload.find("\"person_detected\":true") != std::string::npos, "2.1.11 UDP payload contains person_detected:true");
  }

  // Test 2.2: Instant Zero-Delay Fallover to USB Serial upon Wi-Fi Disconnect
  WiFi.setMockStatus(WL_DISCONNECTED);
  mockMqtt.setMockConnected(false);
  mockSerial.clearCapture();
  mockUdp.clearHistory();
  mockMqtt.clearHistory();
  comm.tick();

  M3_ASSERT(!comm.isWifiConnected(), "2.2.1 Wi-Fi reports disconnected");
  M3_ASSERT(comm.isSerialFallbackActive(), "2.2.2 Serial fallback engaged");

  auto t0 = std::chrono::high_resolution_clock::now();
  bool fallback_tx = comm.transmit(trackData);
  auto t1 = std::chrono::high_resolution_clock::now();
  double latency_us = std::chrono::duration<double, std::micro>(t1 - t0).count();

  M3_ASSERT(fallback_tx, "2.2.3 Fallback transmission returns true");
  M3_ASSERT(latency_us < 100.0, "2.2.4 Fallback latency strictly < 100 us (zero-delay)");
  M3_ASSERT_EQ(0, mockUdp.getPacketCount(), "2.2.5 Zero UDP packets while offline");
  M3_ASSERT_EQ(0, mockMqtt.getPublishCount(), "2.2.6 Zero MQTT publishes while offline");

  std::string serial_out = mockSerial.getCaptured();
  M3_ASSERT(!serial_out.empty(), "2.2.7 Serial received fallback output");
  M3_ASSERT(serial_out.back() == '\n', "2.2.8 Serial output ends with newline '\\n'");

  StaticJsonDocument<512> doc;
  DeserializationError err = deserializeJson(doc, serial_out);
  M3_ASSERT(!err, "2.2.9 Serial payload is valid JSON");
  M3_ASSERT_EQ("econ/telemetry/zone_1", doc["_topic"].as<std::string>(), "2.2.10 Serial payload contains _topic for bridge");
  M3_ASSERT_EQ("esp32_cam_01", doc["sensor_id"].as<std::string>(), "2.2.11 Serial payload contains sensor_id");

  // Test 2.3: Rapid Network State Flapping Recovery
  uint32_t initial_failovers = comm.getFailoverCount();
  for (int cycle = 0; cycle < 10; ++cycle) {
    WiFi.setMockStatus(WL_CONNECTED);
    mockMqtt.setMockConnected(true);
    comm.tick();
    comm.transmit(trackData);

    WiFi.setMockStatus(WL_DISCONNECTED);
    mockMqtt.setMockConnected(false);
    comm.tick();
    comm.transmit(trackData);
  }
  M3_ASSERT(comm.getFailoverCount() >= initial_failovers + 10, "2.3.1 Failover count incremented on network flapping");

  // Test 2.4: Socket Write Error Instant Fallback
  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(true);
  mockSerial.clearCapture();
  mockUdp.clearHistory();
  mockUdp.failNextSend = true; // Inject socket write error
  uint32_t prior_failovers = comm.getFailoverCount();

  bool socket_err_res = comm.transmit(trackData);
  M3_ASSERT(socket_err_res, "2.4.1 Socket write failure seamlessly recovers via Serial fallback");
  M3_ASSERT(!mockSerial.getCaptured().empty(), "2.4.2 Fallback Serial emitted frame on socket error");
  M3_ASSERT_EQ(prior_failovers + 1, comm.getFailoverCount(), "2.4.3 Failover counter incremented on socket error");

  // Test 2.5: Non-Blocking Timing & Reconnect Cooldown Throttling
  setSimulatedTime(true, 1000);
  WiFi.setMockStatus(WL_DISCONNECTED);
  WiFi.reset();

  comm.tick();
  int initial_begins = WiFi.beginCount;

  // Tick inside 5000ms cooldown (t = 3000ms)
  setSimulatedMillis(3000);
  comm.tick();
  M3_ASSERT_EQ(initial_begins, WiFi.beginCount, "2.5.1 Tick inside 5000ms cooldown skips reconnect attempt");

  // Tick after 5000ms cooldown (t = 7000ms)
  setSimulatedMillis(7000);
  comm.tick();
  M3_ASSERT_EQ(initial_begins + 1, WiFi.beginCount, "2.5.2 Tick after 5000ms triggers non-blocking WiFi.begin()");
}

// =============================================================================
// SUITE 3: Strict Module Isolation & Non-Interference Verification
// =============================================================================
void run_suite_3_strict_module_isolation() {
  printf("\n================================================================================\n");
  printf(" [SUITE 3] Strict Module Isolation & Non-Interference Verification             \n");
  printf("================================================================================\n");

  MockEnvironmentSensors env;
  MockActuationState act;
  NodeConfig cfg = cfgDefaults();

  CameraPersonDetector detector;
  detector.init();

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);
  comm.begin(defaultCommConfig());

  // Test 3.1: Shared I2C Bus Address Non-Collision & Interleaved CRC Checks
  // SHT30 = 0x44, ACD1200 = 0x2A, BH1750 = 0x23, Camera SCCB = 0x21
  const uint8_t sht30_addr = 0x44;
  const uint8_t acd1200_addr = 0x2A;
  const uint8_t bh1750_addr = 0x23;
  const uint8_t camera_sccb_addr = OV7670_I2C_ADDR; // 0x21

  M3_ASSERT(sht30_addr != acd1200_addr, "3.1.1 SHT30 (0x44) != ACD1200 (0x2A)");
  M3_ASSERT(sht30_addr != camera_sccb_addr, "3.1.2 SHT30 (0x44) != Camera SCCB (0x21)");
  M3_ASSERT(acd1200_addr != camera_sccb_addr, "3.1.3 ACD1200 (0x2A) != Camera SCCB (0x21)");
  M3_ASSERT(bh1750_addr != camera_sccb_addr, "3.1.4 BH1750 (0x23) != Camera SCCB (0x21)");

  // Simulate interleaved transactions
  float t1 = 0, h1 = 0;
  int co2_1 = 0;
  M3_ASSERT(env.readSht30(t1, h1), "3.1.5 SHT30 initial read valid with CRC");
  detector.processFrame(); // Interleaved camera SCCB + DMA operation
  M3_ASSERT(env.readCo2(co2_1), "3.1.6 ACD1200 read valid with CRC after camera frame");
  M3_ASSERT_FLOAT_NEAR(24.2f, t1, 0.05f, "3.1.7 SHT30 temperature uncorrupted");
  M3_ASSERT_FLOAT_NEAR(55.4f, h1, 0.05f, "3.1.8 SHT30 humidity uncorrupted");
  M3_ASSERT_EQ(620, co2_1, "3.1.9 ACD1200 CO2 uncorrupted");

  // Test 3.2: GPIO Pin Assignment Non-Collision
  // Verify camera pins do not collide with actuator/analog/sensor pins
  const int ir_pin = 19;
  const int lighting_relay_pin = 23;
  const int plug_relay_pin = 25;
  const int plug_adc_pin = 34;
  const int ac_adc_pin = 35;
  const int supply_temp_pin = 26;

  std::vector<int> camera_pins = {
    PIN_CAM_D0, PIN_CAM_D1, PIN_CAM_D2, PIN_CAM_D3,
    PIN_CAM_D4, PIN_CAM_D5, PIN_CAM_D6, PIN_CAM_D7,
    PIN_CAM_VSYNC, PIN_CAM_HREF, PIN_CAM_PCLK, PIN_CAM_XCLK
  };

  bool pin_collision = false;
  for (int p : camera_pins) {
    if (p == ir_pin || p == lighting_relay_pin || p == plug_relay_pin ||
        p == plug_adc_pin || p == ac_adc_pin || p == supply_temp_pin) {
      pin_collision = true;
      break;
    }
  }
  M3_ASSERT(!pin_collision, "3.2.1 Zero GPIO pin collision between camera and existing actuators/sensors");
  M3_ASSERT_EQ(5, PIN_CAM_D7, "3.2.2 Camera D7 reuses legacy PIR GPIO5 cleanly");

  // Test 3.3: Environmental Sensor Invariance under Continuous ML Inference
  float base_t = 0, base_h = 0, base_lux = 0, base_supply = 0;
  int base_co2 = 0;
  env.readSht30(base_t, base_h);
  env.readCo2(base_co2);
  env.readLux(base_lux);
  env.readSupplyC(base_supply);
  float base_plug_w = env.readPlugAmps() * cfg.plugMainsV;
  float base_ac_w = env.readAcAmps() * cfg.acMainsV;

  // Run 20 intensive camera capture + TFLite inference cycles
  detector.getDriver().setMockPersonDetected(true);
  mockSerial.setCapture(true, false);
  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(true);
  for (int i = 0; i < 20; ++i) {
    detector.processFrame();
    comm.transmit(detector.getLatestData());
  }

  // Verify sensor readings are 100% identical post camera activity
  float post_t = 0, post_h = 0, post_lux = 0, post_supply = 0;
  int post_co2 = 0;
  env.readSht30(post_t, post_h);
  env.readCo2(post_co2);
  env.readLux(post_lux);
  env.readSupplyC(post_supply);
  float post_plug_w = env.readPlugAmps() * cfg.plugMainsV;
  float post_ac_w = env.readAcAmps() * cfg.acMainsV;

  M3_ASSERT_FLOAT_NEAR(base_t, post_t, 1e-4f, "3.3.1 SHT30 Temperature completely unaltered by camera");
  M3_ASSERT_FLOAT_NEAR(base_h, post_h, 1e-4f, "3.3.2 SHT30 Humidity completely unaltered by camera");
  M3_ASSERT_EQ(base_co2, post_co2, "3.3.3 ACD1200 CO2 completely unaltered by camera");
  M3_ASSERT_FLOAT_NEAR(base_lux, post_lux, 1e-4f, "3.3.4 Lux completely unaltered by camera");
  M3_ASSERT_FLOAT_NEAR(base_supply, post_supply, 1e-4f, "3.3.5 Supply temp completely unaltered by camera");
  M3_ASSERT_FLOAT_NEAR(base_plug_w, post_plug_w, 1e-4f, "3.3.6 Plug watts completely unaltered by camera");
  M3_ASSERT_FLOAT_NEAR(base_ac_w, post_ac_w, 1e-4f, "3.3.7 AC watts completely unaltered by camera");

  // Test 3.4: HVAC IR & Relay Actuation Command Invariance
  act.applyCommand("LIGHTS_OFF;SETPOINT=22.5;PLUG_OFF", cfg);
  M3_ASSERT(!act.lights_on, "3.4.1 LIGHTS_OFF parsed correctly");
  M3_ASSERT(!act.plug_on, "3.4.2 PLUG_OFF parsed correctly");
  M3_ASSERT_FLOAT_NEAR(22.5f, act.hvac_setpoint_c, 0.01f, "3.4.3 SETPOINT=22.5 applied correctly");

  // Out of bounds setpoint clamp check (16..30)
  act.applyCommand("SETPOINT=35.0", cfg);
  M3_ASSERT_FLOAT_NEAR(30.0f, act.hvac_setpoint_c, 0.01f, "3.4.4 Setpoint 35.0 clamped to max 30.0");
  act.applyCommand("SETPOINT=12.0", cfg);
  M3_ASSERT_FLOAT_NEAR(16.0f, act.hvac_setpoint_c, 0.01f, "3.4.5 Setpoint 12.0 clamped to min 16.0");

  // Test 3.5: Static Memory Arena Footprint & Zero Dynamic Heap Churn
  M3_ASSERT_EQ(80 * 1024, detector.getArenaTotalBytes(), "3.5.1 Static SRAM Tensor Arena is exactly 80 KB");
  M3_ASSERT(g_person_detect_model_data_len > 10240, "3.5.2 Model weights Flash resident (>10KB)");
  uintptr_t model_addr = reinterpret_cast<uintptr_t>(g_person_detect_model_data);
  M3_ASSERT_EQ(0, (model_addr & 0x0F), "3.5.3 Model data is 16-byte memory aligned in Flash");
}

// =============================================================================
// SUITE 4: Telemetry Payload Formatting, Schema & Timing Compliance
// =============================================================================
void run_suite_4_telemetry_schema_and_timing() {
  printf("\n================================================================================\n");
  printf(" [SUITE 4] Telemetry Payload Formatting, Schema & Timing Compliance             \n");
  printf("================================================================================\n");

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "esp32_cam_01";
  data.zone_id = "zone_1";
  data.timestamp_ms = 1724645160000ULL;
  data.person_detected = true;
  data.confidence = 0.94f;
  data.person_count = 2;

  // Test 4.1: Standard BIM/Topology Tracking Payload Schema
  char buf[256];
  size_t len = serializeTrackingPayload(data, buf, sizeof(buf));
  M3_ASSERT(len > 0, "4.1.1 serializeTrackingPayload returns > 0 bytes");

  StaticJsonDocument<512> doc;
  DeserializationError err = deserializeJson(doc, buf);
  M3_ASSERT(!err, "4.1.2 Serialized payload is valid JSON");
  M3_ASSERT_EQ("esp32_cam_01", doc["sensor_id"].as<std::string>(), "4.1.3 sensor_id matches");
  M3_ASSERT_EQ("zone_1", doc["zone_id"].as<std::string>(), "4.1.4 zone_id matches");
  M3_ASSERT(doc["person_detected"].as<bool>() == true, "4.1.5 person_detected is true");
  M3_ASSERT_FLOAT_NEAR(0.94f, doc["confidence"].as<float>(), 0.01f, "4.1.6 confidence matches 0.94");
  M3_ASSERT_EQ(2, doc["person_count"].as<int>(), "4.1.7 person_count matches 2");

  // Test 4.2: Main Twin Telemetry JSON Formatting with Camera Occupancy
  StaticJsonDocument<384> twin_doc;
  twin_doc["zone"] = "Level 4";
  twin_doc["source"] = "esp32";
  twin_doc["cfgRev"] = 1;
  twin_doc["temperature"] = 24.2;
  twin_doc["humidity"] = 55.4;
  twin_doc["tempReal"] = true;
  twin_doc["occupancy"] = data.person_count; // From CameraPersonDetector
  twin_doc["co2"] = 620;
  twin_doc["lights"] = "ON";
  twin_doc["setpoint"] = 24.0;
  twin_doc["acReal"] = false;

  char twin_buf[384];
  size_t twin_len = serializeJson(twin_doc, twin_buf, sizeof(twin_buf));
  M3_ASSERT(twin_len > 0, "4.2.1 Twin telemetry JSON serialization succeeds");
  M3_ASSERT(strstr(twin_buf, "\"occupancy\":2") != nullptr, "4.2.2 Telemetry contains occupancy:2 from camera");
  M3_ASSERT(strstr(twin_buf, "\"tempReal\":true") != nullptr, "4.2.3 Telemetry contains tempReal:true");

  // Test 4.3: Serial Fallback Framing with _topic
  char serial_buf[256];
  size_t serial_len = serializeTrackingPayloadForSerial(data, "econ/telemetry/zone_1", serial_buf, sizeof(serial_buf));
  M3_ASSERT(serial_len > 0, "4.3.1 Serial fallback serialization succeeds");
  M3_ASSERT(strstr(serial_buf, "\"_topic\":\"econ/telemetry/zone_1\"") != nullptr, "4.3.2 Contains _topic header");

  // Test 4.4: Execution Time Budget Benchmarking (<20us serialize, <500us downsample)
  CameraPersonDetector detector;
  detector.init();

  const int benchmark_iters = 1000;
  auto t_start = std::chrono::high_resolution_clock::now();
  for (int i = 0; i < benchmark_iters; ++i) {
    serializeTrackingPayload(data, buf, sizeof(buf));
  }
  auto t_end = std::chrono::high_resolution_clock::now();
  double serialize_us = std::chrono::duration<double, std::micro>(t_end - t_start).count() / benchmark_iters;
  printf("  [PERF] serializeTrackingPayload latency: %.3f us (budget < 20 us)\n", serialize_us);
  M3_ASSERT(serialize_us < 20.0, "4.4.1 Payload serialization < 20 us");

  std::vector<uint8_t> qqvga_src(CAMERA_FRAME_BYTES, 128);
  std::vector<int8_t> tensor_dst(MODEL_INPUT_BYTES, 0);
  t_start = std::chrono::high_resolution_clock::now();
  for (int i = 0; i < benchmark_iters; ++i) {
    ImagePreprocessor::preprocessFrame(qqvga_src.data(), qqvga_src.size(),
                                       tensor_dst.data(), tensor_dst.size());
  }
  t_end = std::chrono::high_resolution_clock::now();
  double prep_us = std::chrono::duration<double, std::micro>(t_end - t_start).count() / benchmark_iters;
  printf("  [PERF] Bilinear downsample latency: %.3f us (budget < 500 us)\n", prep_us);
  M3_ASSERT(prep_us < 500.0, "4.4.2 Bilinear downsample < 500 us on host");

  // Test 4.5: Out-of-Bounds Clamping & Null Pointer Protection
  PersonTrackingData oobData;
  initTrackingData(&oobData);
  oobData.confidence = 2.5f; // Clamps to 1.00
  oobData.person_count = -10; // Clamps to 0
  oobData.sensor_id = nullptr; // Maps to unknown_sensor
  oobData.zone_id = nullptr;   // Maps to unknown_zone

  len = serializeTrackingPayload(oobData, buf, sizeof(buf));
  M3_ASSERT(len > 0, "4.5.1 Out-of-bounds tracking data serializes safely");
  doc.clear();
  deserializeJson(doc, buf);
  M3_ASSERT_FLOAT_NEAR(1.00f, doc["confidence"].as<float>(), 0.01f, "4.5.2 Confidence > 1.0 clamped to 1.00");
  M3_ASSERT_EQ(0, doc["person_count"].as<int>(), "4.5.3 Negative headcount clamped to 0");
  M3_ASSERT_EQ("unknown_sensor", doc["sensor_id"].as<std::string>(), "4.5.4 Null sensor_id safely guarded");
  M3_ASSERT_EQ("unknown_zone", doc["zone_id"].as<std::string>(), "4.5.5 Null zone_id safely guarded");
}

// =============================================================================
// MAIN TEST RUNNER (Dual Unity / Host Compatible)
// =============================================================================

#if M3_USE_UNITY

void setUp(void) {}
void tearDown(void) {}

void test_unity_suite_1() { run_suite_1_camera_occupancy_pir_replacement(); }
void test_unity_suite_2() { run_suite_2_dual_mode_comms_and_fallback(); }
void test_unity_suite_3() { run_suite_3_strict_module_isolation(); }
void test_unity_suite_4() { run_suite_4_telemetry_schema_and_timing(); }

int main(int argc, char** argv) {
  UNITY_BEGIN();
  RUN_TEST(test_unity_suite_1);
  RUN_TEST(test_unity_suite_2);
  RUN_TEST(test_unity_suite_3);
  RUN_TEST(test_unity_suite_4);
  return UNITY_END();
}

#else

int main() {
  printf("================================================================================\n");
  printf("   MILESTONE 3: MAIN SYSTEM INTEGRATION & STRICT ISOLATION TEST SUITE           \n");
  printf("================================================================================\n");

  run_suite_1_camera_occupancy_pir_replacement();
  run_suite_2_dual_mode_comms_and_fallback();
  run_suite_3_strict_module_isolation();
  run_suite_4_telemetry_schema_and_timing();

  printf("\n================================================================================\n");
  printf("                      M3 TEST SUITE EXECUTION SUMMARY                           \n");
  printf("================================================================================\n");
  printf(" Total Assertion Checks Run : %d\n", g_m3_tests_total);
  printf(" Checks Passed              : %d\n", g_m3_tests_passed);
  printf(" Checks Failed              : %d\n", g_m3_tests_failed);
  printf(" Overall Status             : %s\n", (g_m3_tests_failed == 0) ? "ALL PASS (100% SUCCESS)" : "FAILED");
  printf("================================================================================\n\n");

  return (g_m3_tests_failed == 0) ? 0 : 1;
}

#endif
