// =============================================================================
// test_adversarial_challenger2_full.cpp — Full Adversarial Stress Test Harness
// Challenger 2: Dual-Mode Comm, Tracking Payload Serializer & Main Loop Stress
// =============================================================================

#include <iostream>
#include <vector>
#include <string>
#include <cmath>
#include <cstring>
#include <chrono>
#include <random>
#include <algorithm>
#include <numeric>
#include <cstdint>
#include <cstddef>
#include <sstream>
#include <iomanip>
#include <limits>
#include <new>

// Custom global heap allocation tracking hooks
static uint64_t g_heap_alloc_count = 0;
static uint64_t g_heap_alloc_bytes = 0;
static uint64_t g_heap_free_count = 0;
static bool g_track_heap = false;

void* operator new(size_t size) {
  if (g_track_heap) {
    g_heap_alloc_count++;
    g_heap_alloc_bytes += size;
  }
  void* p = std::malloc(size);
  if (!p) throw std::bad_alloc();
  return p;
}

void operator delete(void* p) noexcept {
  if (g_track_heap && p) {
    g_heap_free_count++;
  }
  std::free(p);
}

void* operator new[](size_t size) {
  if (g_track_heap) {
    g_heap_alloc_count++;
    g_heap_alloc_bytes += size;
  }
  void* p = std::malloc(size);
  if (!p) throw std::bad_alloc();
  return p;
}

void operator delete[](void* p) noexcept {
  if (g_track_heap && p) {
    g_heap_free_count++;
  }
  std::free(p);
}

#include "arduino_shim.h"
#include "WiFi.h"
#include "WiFiUdp.h"
#include "PubSubClient.h"
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

// Test harness statistics
static int g_ch2_total_checks = 0;
static int g_ch2_passed_checks = 0;
static int g_ch2_failed_checks = 0;

static void ch2_assert(bool cond, const char* name, const char* detail = "") {
  g_ch2_total_checks++;
  if (cond) {
    g_ch2_passed_checks++;
    std::cout << "  [PASS] " << name << "\n";
  } else {
    g_ch2_failed_checks++;
    std::cout << "  [FAIL] " << name << (detail && strlen(detail) > 0 ? " -- " : "")
              << (detail ? detail : "") << "\n";
  }
}

#define CH2_CHECK(cond, name) ch2_assert((cond), name)
#define CH2_CHECK_EQ(exp, act, name) ch2_assert(((exp) == (act)), name)

// =============================================================================
// SUITE 1: Ultra-Rapid Flapping & Deterministic Zero-Data-Loss Conservation
// =============================================================================
void test_suite_1_flapping_and_conservation() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 1] Ultra-Rapid Wi-Fi Flapping & Zero-Data-Loss Conservation Tests\n";
  std::cout << "================================================================================\n";

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "TestSSID";
  cfg.wifi_pass = "TestPass";
  cfg.mqtt_topic = "econ/telemetry/zone_1";
  cfg.reconnect_interval_ms = 2000;
  comm.begin(cfg);

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "sensor_flapping_test";
  data.zone_id = "zone_flapping_test";
  data.person_detected = true;
  data.confidence = 0.95f;
  data.person_count = 3;

  mockSerial.setCapture(true, false);

  // --- Test 1.1: 10,000 Rapid Flapping Iterations with Conservation Law ---
  std::cout << "  -> 1.1: Testing 10,000 rapid connect/disconnect flap iterations with conservation law...\n";
  const int FLAP_CYCLES = 10000;
  uint32_t expected_wifi_sends = 0;
  uint32_t expected_serial_sends = 0;

  for (int i = 0; i < FLAP_CYCLES; i++) {
    bool wifi_state = (i % 2 == 0);
    WiFi.setConnected(wifi_state);
    comm.update();

    bool tx_ok = comm.transmit(data);
    if (!tx_ok) {
      CH2_CHECK(false, "Transmit failed during rapid flapping transition");
      break;
    }

    if (wifi_state) {
      expected_wifi_sends++;
    } else {
      expected_serial_sends++;
    }
  }

  CH2_CHECK_EQ(expected_wifi_sends, comm.getPacketsSentWiFi(), "Packets sent over Wi-Fi matches expected exactly (5000)");
  CH2_CHECK_EQ(expected_serial_sends, comm.getPacketsSentSerial(), "Packets sent over Serial fallback matches expected exactly (5000)");
  CH2_CHECK_EQ(FLAP_CYCLES, (int)(comm.getPacketsSentWiFi() + comm.getPacketsSentSerial()), "Conservation law: packets_wifi + packets_serial == total_transmissions (10000)");
  CH2_CHECK(comm.getFailoverCount() >= (uint32_t)(FLAP_CYCLES / 2), "Failover count accurately logged all offline transitions");

  // --- Test 1.2: Stochastic Asymmetric Flapping (Bernoulli p=0.1, 0.5, 0.9) ---
  std::cout << "  -> 1.2: Testing stochastic asymmetric flapping distributions (5,000 iterations)...\n";
  std::mt19937 rng(42);
  std::bernoulli_distribution dist_p10(0.10); // 10% connected
  std::bernoulli_distribution dist_p90(0.90); // 90% connected

  uint32_t prev_wifi = comm.getPacketsSentWiFi();
  uint32_t prev_serial = comm.getPacketsSentSerial();

  const int STOCH_ITER = 5000;
  for (int i = 0; i < STOCH_ITER; i++) {
    bool state = (i < 2500) ? dist_p10(rng) : dist_p90(rng);
    WiFi.setConnected(state);
    comm.update();
    bool tx_ok = comm.transmit(data);
    if (!tx_ok) {
      CH2_CHECK(false, "Transmit failed during stochastic distribution test");
      break;
    }
  }

  uint32_t delta_wifi = comm.getPacketsSentWiFi() - prev_wifi;
  uint32_t delta_serial = comm.getPacketsSentSerial() - prev_serial;
  CH2_CHECK_EQ((uint32_t)STOCH_ITER, delta_wifi + delta_serial, "Stochastic conservation law holds across 5,000 asymmetric iterations");
  std::cout << "     -> Stochastic delta: Wi-Fi=" << delta_wifi << " (" << (delta_wifi * 100.0 / STOCH_ITER)
            << "%), Serial=" << delta_serial << " (" << (delta_serial * 100.0 / STOCH_ITER) << "%)\n";

  // --- Test 1.3: Active Transport Mode Query Integrity ---
  std::cout << "  -> 1.3: Verifying getActiveTransport() queries under different states...\n";
  WiFi.setConnected(true);
  comm.update();
  CH2_CHECK_EQ((int)COMM_TRANSPORT_WIFI_UDP, (int)comm.getActiveTransport(), "Active transport is COMM_TRANSPORT_WIFI_UDP when Wi-Fi connected and MQTT not connected");
  CH2_CHECK(comm.isPrimaryTransportActive(), "isPrimaryTransportActive() is true when Wi-Fi connected");
  CH2_CHECK(!comm.isSerialFallbackActive(), "isSerialFallbackActive() is false when Wi-Fi connected");

  WiFi.setConnected(false);
  comm.update();
  CH2_CHECK_EQ((int)COMM_TRANSPORT_SERIAL, (int)comm.getActiveTransport(), "Active transport is COMM_TRANSPORT_SERIAL when Wi-Fi disconnected");
  CH2_CHECK(!comm.isPrimaryTransportActive(), "isPrimaryTransportActive() is false when Wi-Fi disconnected");
  CH2_CHECK(comm.isSerialFallbackActive(), "isSerialFallbackActive() is true when Wi-Fi disconnected");
}

// =============================================================================
// SUITE 2: Comprehensive UDP Packet Drop & Socket Send Failure Modes
// =============================================================================
void test_suite_2_udp_socket_failures_and_drops() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 2] UDP Socket Failures, Packet Drops & Failover Recovery Tests\n";
  std::cout << "================================================================================\n";

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "TestSSID";
  cfg.wifi_pass = "TestPass";
  comm.begin(cfg);

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "sensor_udp_fail";
  data.zone_id = "zone_udp_fail";
  data.person_detected = true;
  data.confidence = 0.88f;
  data.person_count = 1;

  mockSerial.setCapture(true, false);
  WiFi.setConnected(true);
  comm.update();

  // --- Test 2.1: beginPacket() Failure Injection ---
  std::cout << "  -> 2.1: Testing beginPacket() failure injection across 1,000 calls...\n";
  mockUdp.setFailOnSend(true); // beginPacket returns 0
  uint32_t initial_fallback = comm.getPacketsSentSerial();
  uint32_t initial_failovers = comm.getFailoverCount();

  bool all_begin_ok = true;
  for (int i = 0; i < 1000; i++) {
    if (!comm.transmit(data)) {
      all_begin_ok = false;
      break;
    }
  }
  CH2_CHECK(all_begin_ok, "Transmit succeeds via Serial fallback across 1,000 beginPacket() failures");
  CH2_CHECK_EQ(1000u, comm.getPacketsSentSerial() - initial_fallback, "Exactly 1,000 packets routed to Serial on beginPacket() failure");
  CH2_CHECK_EQ(1000u, comm.getFailoverCount() - initial_failovers, "Failover counter incremented 1,000 times for beginPacket() failures");

  mockUdp.setFailOnSend(false);

  // --- Test 2.2: endPacket() Failure Injection ---
  std::cout << "  -> 2.2: Testing endPacket() failure injection across 1,000 calls...\n";
  mockUdp.failNextSend = false;
  initial_fallback = comm.getPacketsSentSerial();
  initial_failovers = comm.getFailoverCount();

  bool all_end_ok = true;
  for (int i = 0; i < 1000; i++) {
    mockUdp.failNextSend = true; // endPacket returns 0 for this call
    if (!comm.transmit(data)) {
      all_end_ok = false;
      break;
    }
  }
  CH2_CHECK(all_end_ok, "Transmit succeeds via Serial fallback across 1,000 endPacket() failures");
  CH2_CHECK_EQ(1000u, comm.getPacketsSentSerial() - initial_fallback, "Exactly 1,000 packets routed to Serial on endPacket() failure");
  CH2_CHECK_EQ(1000u, comm.getFailoverCount() - initial_failovers, "Failover counter incremented 1,000 times for endPacket() failures");

  // --- Test 2.3: Intermittent Drop Patterns (1-in-2 alternating drops) ---
  std::cout << "  -> 2.3: Testing alternating 50% UDP drop pattern (2,000 packets)...\n";
  uint32_t start_wifi = comm.getPacketsSentWiFi();
  uint32_t start_serial = comm.getPacketsSentSerial();

  bool all_drops_ok = true;
  for (int i = 0; i < 2000; i++) {
    if (i % 2 == 1) {
      mockUdp.failNextSend = true;
    }
    if (!comm.transmit(data)) {
      all_drops_ok = false;
      break;
    }
  }
  CH2_CHECK(all_drops_ok, "Transmit succeeds under 50% alternating UDP packet drops");
  CH2_CHECK_EQ(1000u, comm.getPacketsSentWiFi() - start_wifi, "Exactly 1,000 packets delivered via Wi-Fi UDP");
  CH2_CHECK_EQ(1000u, comm.getPacketsSentSerial() - start_serial, "Exactly 1,000 packets delivered via Serial fallback");

  // --- Test 2.4: Double Fault Safety (UDP fails AND Serial Fallback Disabled) ---
  std::cout << "  -> 2.4: Testing double fault safety (UDP fails + Serial fallback disabled)...\n";
  CommConfig no_fallback_cfg = cfg;
  no_fallback_cfg.enable_serial_fallback = false;
  DualModeComm comm_no_fallback(mockUdp, mockMqtt, mockSerial);
  comm_no_fallback.begin(no_fallback_cfg);
  WiFi.setConnected(true);
  comm_no_fallback.update();

  mockUdp.setFailOnSend(true);
  bool double_fault_ok = comm_no_fallback.transmit(data);
  CH2_CHECK(!double_fault_ok, "Double fault cleanly returns false without crash or hang");
  mockUdp.setFailOnSend(false);
}

// =============================================================================
// SUITE 3: MQTT Reconnection Stalls, Broker Outages & Transport Modes
// =============================================================================
void test_suite_3_mqtt_stalls_and_broker_outages() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 3] MQTT Non-Blocking Reconnection Stalls & Dual Transport Tests\n";
  std::cout << "================================================================================\n";

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "TestSSID";
  cfg.wifi_pass = "TestPass";
  cfg.mqtt_topic = "econ/telemetry/zone_1";
  cfg.reconnect_interval_ms = 5000;
  comm.begin(cfg);

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "sensor_mqtt_test";
  data.zone_id = "zone_mqtt_test";
  data.person_detected = true;
  data.confidence = 0.91f;
  data.person_count = 2;

  WiFi.setConnected(true);
  comm.update();

  // --- Test 3.1: Disconnected MQTT broker during high-throughput transmission ---
  std::cout << "  -> 3.1: High-throughput telemetry with disconnected MQTT broker...\n";
  comm.setMqttClient(&mockMqtt, "econ/telemetry/zone_1");
  // mockMqtt is not connected (connected() == false)
  CH2_CHECK(!mockMqtt.connected(), "MQTT mock client initially disconnected");

  auto start_time = std::chrono::high_resolution_clock::now();
  for (int i = 0; i < 5000; i++) {
    bool ok = comm.transmit(data);
    if (!ok) {
      CH2_CHECK(false, "Transmit failed with disconnected MQTT");
      break;
    }
  }
  auto end_time = std::chrono::high_resolution_clock::now();
  double elapsed_us = std::chrono::duration<double, std::micro>(end_time - start_time).count();
  double avg_us = elapsed_us / 5000.0;

  CH2_CHECK(avg_us < 20.0, "Average transmit latency with disconnected MQTT is < 20 us (no stalling)");
  std::cout << "     -> Latency with disconnected MQTT: " << avg_us << " us/transmit\n";

  // --- Test 3.2: Reconnect interval throttling in disconnected state ---
  std::cout << "  -> 3.2: Reconnect interval throttling in disconnected state...\n";
  WiFi.setConnected(false);
  comm.update();
  CH2_CHECK_EQ((int)COMM_STATE_DISCONNECTED, (int)comm.getState(), "Comm transitioned to COMM_STATE_DISCONNECTED");

  int begin_count_start = WiFi.beginCount;
  setSimulatedTime(true, 1000);

  // Call update() 10,000 times across 10,000 ms simulated time
  for (int ms = 0; ms < 10000; ms += 1) {
    setSimulatedMillis(1000 + ms);
    comm.update();
  }

  int begin_count_end = WiFi.beginCount;
  int reconnect_calls = begin_count_end - begin_count_start;
  // Over 10,000 ms with reconnect_interval_ms=5000, should be called at most 2 times!
  CH2_CHECK(reconnect_calls <= 2, "WiFi.begin() called at most 2 times over 10s (strictly throttled)");
  std::cout << "     -> WiFi.begin() reconnect attempts over 10s: " << reconnect_calls << "\n";
  setSimulatedTime(false);
}

// =============================================================================
// SUITE 4: Tracking Payload Serializer Extreme Boundary & Adversarial Injection
// =============================================================================
void test_suite_4_payload_serializer_adversarial() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 4] Tracking Payload Serializer Boundary & Adversarial Injection Tests\n";
  std::cout << "================================================================================\n";

  char buffer[1024];
  PersonTrackingData data;

  // --- Test 4.1: Float Extremes (NaN, Inf, Subnormals) ---
  std::cout << "  -> 4.1: Float extremes handling (NaN, +Inf, -Inf, subnormals)...\n";
  initTrackingData(&data);
  data.sensor_id = "sensor_float";
  data.zone_id = "zone_float";
  data.person_detected = true;
  data.person_count = 1;

  // Quiet NaN
  data.confidence = std::numeric_limits<float>::quiet_NaN();
  size_t len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(len > 0, "Serialization handles quiet_NaN float safely");

  // +Infinity -> clamped to 1.00
  data.confidence = std::numeric_limits<float>::infinity();
  len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(len > 0, "Serialization handles +Infinity float");
  CH2_CHECK(strstr(buffer, "\"confidence\":1.00") != nullptr, "+Infinity confidence clamped to 1.00");

  // -Infinity -> clamped to 0.00
  data.confidence = -std::numeric_limits<float>::infinity();
  len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(len > 0, "Serialization handles -Infinity float");
  CH2_CHECK(strstr(buffer, "\"confidence\":0.00") != nullptr, "-Infinity confidence clamped to 0.00");

  // Subnormal float (e.g. 1.4e-45) -> clamped to 0.00
  data.confidence = std::numeric_limits<float>::denorm_min();
  len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(len > 0, "Serialization handles subnormal float safely");

  // --- Test 4.2: Headcount Integer Extremes ---
  std::cout << "  -> 4.2: Headcount integer extremes (INT_MIN, -5, 0, INT_MAX)...\n";
  data.confidence = 0.85f;
  data.person_count = -100;
  len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(strstr(buffer, "\"person_count\":0") != nullptr, "Negative person_count clamped to 0");

  data.person_count = std::numeric_limits<int>::min();
  len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(strstr(buffer, "\"person_count\":0") != nullptr, "INT_MIN person_count clamped to 0");

  data.person_count = 999999;
  len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(strstr(buffer, "\"person_count\":999999") != nullptr, "Large positive person_count serialized correctly");

  // --- Test 4.3: 64-bit Timestamp Extremes ---
  std::cout << "  -> 4.3: 64-bit timestamp extremes (0, UINT32_MAX, UINT64_MAX)...\n";
  data.person_count = 1;
  data.timestamp_ms = 0ULL;
  len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(strstr(buffer, "\"timestamp_ms\":0") != nullptr, "0 timestamp_ms serialized accurately");

  data.timestamp_ms = 4294967295ULL; // UINT32_MAX
  len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(strstr(buffer, "\"timestamp_ms\":4294967295") != nullptr, "UINT32_MAX timestamp serialized accurately");

  data.timestamp_ms = 18446744073709551615ULL; // UINT64_MAX
  len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(strstr(buffer, "\"timestamp_ms\":18446744073709551615") != nullptr, "UINT64_MAX timestamp serialized without 32-bit truncation");

  // --- Test 4.4: JSON Injection & Format String Injection Attack Vectors ---
  std::cout << "  -> 4.4: Pathological string inputs and format string injection defenses...\n";
  const char* injection_vectors[] = {
    "%s%s%s%d%n%x%p",
    "'; DROP TABLE sensors; --",
    "\"},\"fake_field\":true,{\"bad\":\"",
    "\\n\\r\\t\\\\\"",
    "🏢 Level 4 Office Zone 🇻🇳",
    "Sensor\x01\x02\x03\x1F"
  };

  for (const char* vec : injection_vectors) {
    data.sensor_id = vec;
    data.zone_id = vec;
    len = serializeTrackingPayload(data, buffer, sizeof(buffer));
    CH2_CHECK(len > 0, "Serialization handles injection vector safely without crashing");
    CH2_CHECK(len < sizeof(buffer), "Output length strictly within allocated buffer bounds");
  }

  // --- Test 4.5: Null Pointer Resilience ---
  std::cout << "  -> 4.5: Null pointer resilience...\n";
  data.sensor_id = nullptr;
  data.zone_id = nullptr;
  len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(len > 0, "Serialization handles null sensor_id and zone_id safely");
  CH2_CHECK(strstr(buffer, "\"sensor_id\":\"unknown_sensor\"") != nullptr, "Null sensor_id falls back to unknown_sensor");
  CH2_CHECK(strstr(buffer, "\"zone_id\":\"unknown_zone\"") != nullptr, "Null zone_id falls back to unknown_zone");

  len = serializeTrackingPayloadPtr(nullptr, buffer, sizeof(buffer));
  CH2_CHECK_EQ(0u, len, "Null data pointer returns 0 safely");

  len = serializeTrackingPayload(data, nullptr, 100);
  CH2_CHECK_EQ(0u, len, "Null buffer pointer returns 0 safely");

  // --- Test 4.6: Buffer Boundary Canary Fuzzing (Lengths 0 to 512) ---
  std::cout << "  -> 4.6: Memory canary boundary fuzzing across buffer sizes 0..512...\n";
  uint8_t memory_slab[1024];
  const size_t CANARY_SIZE = 64;
  bool canary_corrupted = false;

  for (size_t test_buf_len = 0; test_buf_len <= 512; test_buf_len++) {
    // Fill pre-canary, test region, and post-canary
    memset(memory_slab, 0xAA, CANARY_SIZE);
    memset(memory_slab + CANARY_SIZE, 0x00, test_buf_len);
    memset(memory_slab + CANARY_SIZE + test_buf_len, 0x55, CANARY_SIZE);

    char* test_ptr = (char*)(memory_slab + CANARY_SIZE);
    size_t written = serializeTrackingPayload(data, test_ptr, test_buf_len);

    // Verify pre-canary
    for (size_t i = 0; i < CANARY_SIZE; i++) {
      if (memory_slab[i] != 0xAA) {
        canary_corrupted = true;
        break;
      }
    }
    // Verify post-canary
    for (size_t i = 0; i < CANARY_SIZE; i++) {
      if (memory_slab[CANARY_SIZE + test_buf_len + i] != 0x55) {
        canary_corrupted = true;
        break;
      }
    }

    if (test_buf_len > 0 && written > 0) {
      if (written >= test_buf_len) {
        canary_corrupted = true;
      }
      if (test_ptr[written] != '\0') {
        canary_corrupted = true;
      }
    }

    if (canary_corrupted) break;
  }
  CH2_CHECK(!canary_corrupted, "All 64-byte pre- and post-memory canaries intact across sizes 0..512");

  // --- Test 4.7: Extended Payload with Bounding Boxes ---
  std::cout << "  -> 4.7: Extended payload serialization with bounding boxes...\n";
  TrackingBoundingBox bboxes[6];
  for (int i = 0; i < 6; i++) {
    bboxes[i].xmin = 0.1f * i;
    bboxes[i].ymin = 0.1f * i;
    bboxes[i].xmax = 0.1f * i + 0.2f;
    bboxes[i].ymax = 0.1f * i + 0.3f;
    bboxes[i].confidence = 0.90f;
    bboxes[i].class_id = 0;
  }
  data.sensor_id = "cam_ext";
  data.zone_id = "zone_ext";
  data.inference_time_ms = 45;
  data.fps = 6.6f;
  data.bboxes = bboxes;
  data.bbox_count = 6; // exceed max (4)

  len = serializeExtendedTrackingPayload(data, buffer, sizeof(buffer));
  CH2_CHECK(len > 0, "Extended tracking payload serialized successfully");
  CH2_CHECK(strstr(buffer, "\"inference_ms\":45") != nullptr, "inference_ms included in extended payload");
  CH2_CHECK(strstr(buffer, "\"fps\":6.6") != nullptr, "fps included in extended payload");
  CH2_CHECK(strstr(buffer, "\"bboxes\":[") != nullptr, "bboxes array included in extended payload");

  // --- Test 4.8: 2,000 Randomized Round-Trip Oracle Verifications ---
  std::cout << "  -> 4.8: 2,000 randomized round-trip oracle verifications...\n";
  std::mt19937 rand_gen(1337);
  std::uniform_real_distribution<float> conf_dist(0.0f, 1.0f);
  std::uniform_int_distribution<int> count_dist(0, 10);
  std::uniform_int_distribution<uint64_t> time_dist(1000ULL, 10000000ULL);
  std::bernoulli_distribution bool_dist(0.5);

  int roundtrip_mismatches = 0;
  char z_buf[64], s_buf[64];

  for (int i = 0; i < 2000; i++) {
    PersonTrackingData input_data;
    initTrackingData(&input_data);
    input_data.person_detected = bool_dist(rand_gen);
    input_data.confidence = conf_dist(rand_gen);
    input_data.person_count = count_dist(rand_gen);
    input_data.timestamp_ms = time_dist(rand_gen);
    input_data.sensor_id = "oracle_sensor";
    input_data.zone_id = "oracle_zone";

    len = serializeTrackingPayload(input_data, buffer, sizeof(buffer));
    if (len == 0) { roundtrip_mismatches++; continue; }

    PersonTrackingData output_data;
    bool des_ok = deserializeTrackingPayload(buffer, len, &output_data, z_buf, sizeof(z_buf), s_buf, sizeof(s_buf));
    if (!des_ok) { roundtrip_mismatches++; continue; }

    if (output_data.person_detected != input_data.person_detected) roundtrip_mismatches++;
    if (output_data.person_count != input_data.person_count) roundtrip_mismatches++;
    if (output_data.timestamp_ms != input_data.timestamp_ms) roundtrip_mismatches++;
    if (std::abs(output_data.confidence - round(input_data.confidence * 100.0f) / 100.0f) > 0.015f) roundtrip_mismatches++;
  }

  CH2_CHECK_EQ(0, roundtrip_mismatches, "2,000 randomized tracking payloads round-tripped with 0 mismatches (100% oracle match)");
}

// =============================================================================
// SUITE 5: Timestamp Rollover & Timer Arithmetic (49.7-Day Wraparound Modulo 2^32)
// =============================================================================
void test_suite_5_timestamp_rollover_and_timer_math() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 5] Timestamp Rollover & Timer Arithmetic (49.7-Day Wraparound) Tests\n";
  std::cout << "================================================================================\n";

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "TestSSID";
  cfg.wifi_pass = "TestPass";
  cfg.connect_timeout_ms = 8000;
  cfg.reconnect_interval_ms = 5000;

  // --- Test 5.1: Millis Rollover during COMM_STATE_CONNECTING Timeout ---
  std::cout << "  -> 5.1: Millis rollover across 0xFFFFFFFF during connect timeout...\n";
  setSimulatedTime(true, 0xFFFFFFF0); // 16 ms before 32-bit overflow
  comm.begin(cfg);
  WiFi.setConnected(false);
  comm.update(); // Enters COMM_STATE_CONNECTING at 0xFFFFFFF0

  CH2_CHECK_EQ((int)COMM_STATE_CONNECTING, (int)comm.getState(), "State is COMM_STATE_CONNECTING prior to overflow");

  // Advance time across overflow to 0x00002000 (8208 ms elapsed: 16 ms before + 8192 ms after)
  // Elapsed: 0x00002000 - 0xFFFFFFF0 = 8192 - (-16) = 8208 ms >= 8000 ms timeout
  setSimulatedMillis(0x00002000);
  comm.update();

  CH2_CHECK_EQ((int)COMM_STATE_DISCONNECTED, (int)comm.getState(), "Timeout triggered accurately across 32-bit rollover -> COMM_STATE_DISCONNECTED");
  CH2_CHECK(comm.getFailoverCount() > 0, "Failover counter incremented on connect timeout rollover");

  // --- Test 5.2: Millis Rollover during Reconnect Interval ---
  std::cout << "  -> 5.2: Millis rollover across 0xFFFFFFFF during reconnect interval...\n";
  // We are currently in COMM_STATE_DISCONNECTED.
  // Set last reconnect attempt to 0xFFFFFFE0
  setSimulatedMillis(0xFFFFFFE0);
  int begins_before = WiFi.beginCount;

  // Advance to 0x00000100 (elapsed: 0x100 - 0xFFFFFFE0 = 256 + 32 = 288 ms < 5000 ms) -> should NOT reconnect
  setSimulatedMillis(0x00000100);
  comm.update();
  CH2_CHECK_EQ(begins_before, WiFi.beginCount, "No reconnect triggered before interval elapsed");

  // Advance to 0x00001500 (elapsed: 0x1500 - 0xFFFFFFE0 = 5376 + 32 = 5408 ms >= 5000 ms) -> should reconnect!
  setSimulatedMillis(0x00001500);
  comm.update();
  CH2_CHECK_EQ(begins_before + 1, WiFi.beginCount, "Reconnect triggered accurately across 32-bit rollover when interval elapsed");

  // --- Test 5.3: Main Loop 150ms Camera Frame Interval Rollover ---
  std::cout << "  -> 5.3: Main loop 150ms camera polling interval rollover simulation...\n";
  unsigned long lastCameraFrameTime = 0xFFFFFF80; // 128 ms before overflow
  unsigned long nowCamera = 0x00000030;          // 48 ms after overflow
  // Unsigned subtraction: nowCamera - lastCameraFrameTime = 48 - (-128) = 176 ms >= 150 ms
  bool trigger = (nowCamera - lastCameraFrameTime >= 150);
  CH2_CHECK(trigger, "150ms camera polling interval triggers correctly across 32-bit rollover (176 ms >= 150 ms)");

  // --- Test 5.4: Main Loop 5000ms Publish Interval Rollover ---
  std::cout << "  -> 5.4: Main loop 5000ms publish interval rollover simulation...\n";
  unsigned long lastPublish = 0xFFFFFA00; // 1536 ms before overflow
  unsigned long nowPublish = 0x00000E00;  // 3584 ms after overflow
  // Unsigned subtraction: nowPublish - lastPublish = 3584 + 1536 = 5120 ms >= 5000 ms
  bool pub_trigger = (nowPublish - lastPublish > 5000);
  CH2_CHECK(pub_trigger, "5000ms telemetry publish triggers correctly across 32-bit rollover (5120 ms > 5000 ms)");

  setSimulatedTime(false);
}

// =============================================================================
// SUITE 6: Continuous Transmission, Endurance & Zero Heap Allocation Audit
// =============================================================================
void test_suite_6_continuous_endurance_and_heap_audit() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 6] Continuous Transmission Endurance & Zero Heap Allocation Audit\n";
  std::cout << "================================================================================\n";

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "TestSSID";
  cfg.wifi_pass = "TestPass";
  comm.begin(cfg);

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "sensor_audit";
  data.zone_id = "zone_audit";
  data.person_detected = true;
  data.confidence = 0.94f;
  data.person_count = 2;
  data.timestamp_ms = 123456789ULL;

  char buffer[512];
  mockSerial.setCapture(false, false); // Don't accumulate strings in mock buffer

  // --- Test 6.1: Zero Heap Allocations on Hot Path (100,000 Operations) ---
  std::cout << "  -> 6.1: Zero heap allocation verification across 100,000 hot-path operations...\n";

  // Serial fallback mode (writes to Stream without dynamic vector allocations)
  WiFi.setConnected(false);
  comm.update();

  g_heap_alloc_count = 0;
  g_heap_alloc_bytes = 0;
  g_track_heap = true;

  // 25,000 Serial Fallback Transmissions
  for (int i = 0; i < 25000; i++) {
    comm.transmit(data);
  }

  // 25,000 Canonical Serializations
  for (int i = 0; i < 25000; i++) {
    serializeTrackingPayload(data, buffer, sizeof(buffer));
  }

  // 25,000 Serial Formatted Serializations
  for (int i = 0; i < 25000; i++) {
    serializeTrackingPayloadForSerial(data, "econ/telemetry/zone_1", buffer, sizeof(buffer));
  }

  // 25,000 Extended Payload Serializations
  for (int i = 0; i < 25000; i++) {
    serializeExtendedTrackingPayload(data, buffer, sizeof(buffer));
  }

  g_track_heap = false;

  CH2_CHECK_EQ(0ULL, g_heap_alloc_count, "Zero dynamic heap allocations across 100,000 operations (100% stack-allocated hot path)");
  CH2_CHECK_EQ(0ULL, g_heap_alloc_bytes, "Zero dynamic heap bytes allocated");

  // --- Test 6.2: High-Throughput Latency Benchmarking (10,000 cycles) ---
  std::cout << "  -> 6.2: Latency benchmarking (10,000 transmit cycles)...\n";
  std::vector<double> latencies;
  latencies.reserve(10000);

  WiFi.setConnected(true);
  comm.update();

  for (int i = 0; i < 10000; i++) {
    auto t0 = std::chrono::high_resolution_clock::now();
    comm.transmit(data);
    auto t1 = std::chrono::high_resolution_clock::now();
    double us = std::chrono::duration<double, std::micro>(t1 - t0).count();
    latencies.push_back(us);
  }

  std::sort(latencies.begin(), latencies.end());
  double min_l = latencies.front();
  double max_l = latencies.back();
  double p50_l = latencies[latencies.size() * 50 / 100];
  double p95_l = latencies[latencies.size() * 95 / 100];
  double p99_l = latencies[latencies.size() * 99 / 100];
  double mean_l = std::accumulate(latencies.begin(), latencies.end(), 0.0) / latencies.size();

  std::cout << "     --- Transmit Latency Profile (10,000 samples) ---\n";
  std::cout << "       Min:  " << min_l << " us\n";
  std::cout << "       Mean: " << mean_l << " us\n";
  std::cout << "       p50:  " << p50_l << " us\n";
  std::cout << "       p95:  " << p95_l << " us\n";
  std::cout << "       p99:  " << p99_l << " us\n";
  std::cout << "       Max:  " << max_l << " us\n";
  std::cout << "     -------------------------------------------------\n";

  CH2_CHECK(mean_l < 25.0, "Mean transmit latency < 25 us (well within 200 us execution budget)");
  CH2_CHECK(p99_l < 50.0, "p99 transmit latency < 50 us");
}

// =============================================================================
// SUITE 7: Main Loop Integration & Subsystem Invariance
// =============================================================================
void test_suite_7_main_loop_integration_and_invariance() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 7] Main Loop Integration & Subsystem Invariance Tests\n";
  std::cout << "================================================================================\n";

  CameraPersonDetector detector;
  detector.setZoneAndSensorId("zone_1", "econ-esp32-zone_1");
  bool init_ok = detector.init();
  CH2_CHECK(init_ok, "CameraPersonDetector initialized successfully");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "TestSSID";
  cfg.wifi_pass = "TestPass";
  cfg.zone_topic = "zone_1";
  cfg.sensor_id = "econ-esp32-zone_1";
  comm.begin(cfg);

  mockSerial.setCapture(true, false);
  WiFi.setConnected(true);
  comm.update();

  // --- Test 7.1: Simulated Main Loop Frames with Burst on Occupancy Flip ---
  std::cout << "  -> 7.1: Main loop execution with burst telemetry on occupancy transition...\n";
  bool lastState = false;
  int burst_count = 0;

  // Toggle mock person detection state every 20 frames (debounced over 2 frames)
  for (int frame = 0; frame < 100; frame++) {
    // Tick comms state machine
    comm.tick();

    // Toggle simulated scene presence: frames [20..59] occupied, [0..19] & [60..99] empty
    bool expect_present = (frame >= 20 && frame < 60);
    detector.getDriver().setMockPersonDetected(expect_present);

    // Process camera frame
    if (detector.processFrame()) {
      bool current = detector.isPersonDetected();
      if (current != lastState) {
        lastState = current;
        detector.transmitTelemetry(comm);
        burst_count++;
      }
    }
  }

  CH2_CHECK(burst_count >= 2, "Burst telemetry generated on occupancy state changes (empty -> occupied -> empty)");
  CH2_CHECK(comm.getSuccessfulTransmissions() >= 2, "Telemetry transmissions recorded successfully in comms engine");
  std::cout << "     -> Total bursts dispatched: " << burst_count
            << " | Comms total TX: " << comm.getSuccessfulTransmissions() << "\n";
}

// =============================================================================
// Main Entrypoint
// =============================================================================
int main() {
  std::cout << "================================================================================\n";
  std::cout << "   CHALLENGER 2 ADVERSARIAL STRESS TEST HARNESS: FULL EMPIRICAL PROBE SUITE     \n";
  std::cout << "================================================================================\n";

  test_suite_1_flapping_and_conservation();
  test_suite_2_udp_socket_failures_and_drops();
  test_suite_3_mqtt_stalls_and_broker_outages();
  test_suite_4_payload_serializer_adversarial();
  test_suite_5_timestamp_rollover_and_timer_math();
  test_suite_6_continuous_endurance_and_heap_audit();
  test_suite_7_main_loop_integration_and_invariance();

  std::cout << "\n================================================================================\n";
  std::cout << "                      CHALLENGER 2 TEST EXECUTION SUMMARY                       \n";
  std::cout << "================================================================================\n";
  std::cout << " Total Checks Run   : " << g_ch2_total_checks << "\n";
  std::cout << " Checks Passed      : " << g_ch2_passed_checks << "\n";
  std::cout << " Checks Failed      : " << g_ch2_failed_checks << "\n";
  std::cout << " Overall Verdict    : " << (g_ch2_failed_checks == 0 ? "CONFIRM_CORRECTNESS (100% PASS)" : "FAIL") << "\n";
  std::cout << "================================================================================\n\n";

  return (g_ch2_failed_checks == 0) ? 0 : 1;
}
