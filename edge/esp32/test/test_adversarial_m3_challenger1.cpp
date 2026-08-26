// =============================================================================
// test_adversarial_m3_challenger1.cpp — Adversarial Stress Test Suite
// Milestone 3: Dual-Mode Network Stress & Failover Challenger (Challenger 1)
//
// Test Vectors:
//   Suite 1: Ultra-Rapid Network Flapping & Asymmetric Jitter (50+ to 1,000+ flaps)
//   Suite 2: Comprehensive Socket Send Failures & UDP Packet Drop Injection
//   Suite 3: Serial Backpressure, Buffer Overruns & Telemetry Serialization Integrity
//   Suite 4: Empirical Failover Latency Benchmarking (<100 µs Requirement)
//   Suite 5: Integrated System Stress (Camera Person Detection + DualModeComm)
// =============================================================================

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

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cmath>
#include <limits>
#include <chrono>
#include <string>
#include <vector>
#include <algorithm>
#include <numeric>
#include <random>
#include <sstream>
#include <iostream>

// =============================================================================
// Test Framework Infrastructure
// =============================================================================
static int g_challenger_total = 0;
static int g_challenger_passed = 0;
static int g_challenger_failed = 0;

static void chall_check(bool condition, const char* name, const char* detail = "") {
  g_challenger_total++;
  if (condition) {
    g_challenger_passed++;
    printf("  [PASS] %s\n", name);
  } else {
    g_challenger_failed++;
    printf("  [FAIL] %s %s%s\n", name, (detail && strlen(detail) > 0) ? "-- " : "", detail ? detail : "");
  }
}

#define CHALL_ASSERT(cond, msg) chall_check((cond), msg)
#define CHALL_ASSERT_EQ(exp, act, msg) chall_check(((exp) == (act)), msg)
#define CHALL_ASSERT_FLOAT_NEAR(exp, act, eps, msg) \
  chall_check((std::abs((float)(exp) - (float)(act)) <= (float)(eps)), msg)

// =============================================================================
// SUITE 1: Ultra-Rapid Network Flapping & Asymmetric Jitter (50+ to 1,000+ flaps)
// =============================================================================
void run_suite_1_network_flapping_stress() {
  printf("\n================================================================================\n");
  printf(" [SUITE 1] Ultra-Rapid Network Flapping & Asymmetric Jitter Stress Tests        \n");
  printf("================================================================================\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Flapping_IoT_Mesh";
  cfg.wifi_pass = "P@ssw0rd99";
  cfg.mqtt_topic = "econ/telemetry/zone_flap";
  cfg.reconnect_interval_ms = 3000;
  comm.begin(cfg);

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "cam_flap_node";
  data.zone_id = "zone_flap";
  data.person_detected = true;
  data.confidence = 0.93f;
  data.person_count = 2;

  mockSerial.setCapture(true, false);

  // 1.1: 50+ Rapid Symmetric Flaps (100 State Transitions in Quick Succession)
  printf("  -> 1.1: Testing 60 rapid connect/disconnect cycles (120 state transitions)...\n");
  const int rapid_cycles = 60; // > 50 requirement
  bool rapid_flap_clean = true;
  uint32_t expected_wifi = 0;
  uint32_t expected_serial = 0;

  for (int i = 0; i < rapid_cycles; ++i) {
    data.timestamp_ms = 1000ULL + i;

    // Connect
    WiFi.setMockStatus(WL_CONNECTED);
    mockMqtt.setMockConnected(true);
    comm.tick();

    if (!comm.isWifiConnected() || !comm.isPrimaryTransportActive() || comm.isSerialFallbackActive()) {
      rapid_flap_clean = false;
    }

    mockSerial.clearCapture();
    mockUdp.clearHistory();
    bool tx_on = comm.transmit(data);
    if (!tx_on || mockUdp.getPacketCount() != 1 || !mockSerial.getCaptured().empty()) {
      rapid_flap_clean = false;
    }
    expected_wifi++;

    // Disconnect
    WiFi.setMockStatus(WL_DISCONNECTED);
    mockMqtt.setMockConnected(false);
    comm.tick();

    if (comm.isWifiConnected() || comm.isPrimaryTransportActive() || !comm.isSerialFallbackActive()) {
      rapid_flap_clean = false;
    }

    mockSerial.clearCapture();
    mockUdp.clearHistory();
    bool tx_off = comm.transmit(data);
    if (!tx_off || mockUdp.getPacketCount() != 0 || mockSerial.getCaptured().empty()) {
      rapid_flap_clean = false;
    }
    expected_serial++;
  }

  CHALL_ASSERT(rapid_flap_clean, "1.1.1 All 60 rapid flap cycles (120 state changes) maintained perfect state invariants");
  CHALL_ASSERT_EQ(expected_wifi, comm.getSuccessfulTransmissions(), "1.1.2 Exactly 60 packets sent over Wi-Fi UDP broadcast");
  CHALL_ASSERT_EQ(expected_serial, comm.getFallbackTransmissions(), "1.1.3 Exactly 60 packets sent over USB Serial fallback");
  CHALL_ASSERT(comm.getFailoverCount() >= (uint32_t)rapid_cycles, "1.1.4 Failover count accurately logged all 60 offline transitions");

  // 1.2: 1,000 High-Frequency Continuous Alternating Flaps
  printf("  -> 1.2: Testing 1,000 continuous alternating flaps with telemetry conservation...\n");
  uint32_t base_wifi = comm.getSuccessfulTransmissions();
  uint32_t base_serial = comm.getFallbackTransmissions();
  const int flaps_1k = 1000;
  bool zero_loss_1k = true;

  for (int i = 0; i < flaps_1k; ++i) {
    data.timestamp_ms = 2000ULL + i;
    bool online = (i % 2 == 0);
    WiFi.setMockStatus(online ? WL_CONNECTED : WL_DISCONNECTED);
    mockMqtt.setMockConnected(online);
    comm.tick();

    if (!comm.transmit(data)) {
      zero_loss_1k = false;
    }
  }

  uint32_t delta_wifi = comm.getSuccessfulTransmissions() - base_wifi;
  uint32_t delta_serial = comm.getFallbackTransmissions() - base_serial;

  CHALL_ASSERT(zero_loss_1k, "1.2.1 1,000 rapid flaps completed with 0% packet loss");
  CHALL_ASSERT_EQ(500, (int)delta_wifi, "1.2.2 Exactly 500 Wi-Fi transmissions in 1,000-flap loop");
  CHALL_ASSERT_EQ(500, (int)delta_serial, "1.2.3 Exactly 500 Serial fallback transmissions in 1,000-flap loop");
  CHALL_ASSERT_EQ(flaps_1k, (int)(delta_wifi + delta_serial), "1.2.4 Conservation law holds: delta_wifi + delta_serial == 1,000");

  // 1.3: Asymmetric Chaotic Flapping (Markov Bursts: 90% drop bursts, 10% connection windows)
  printf("  -> 1.3: Testing asymmetric chaotic drop bursts (5,000 iterations)...\n");
  std::mt19937 rng(777);
  std::uniform_int_distribution<int> dist_burst(0, 9); // 0 = connected (10%), 1..9 = disconnected (90%)
  
  uint32_t chaotic_wifi = 0;
  uint32_t chaotic_serial = 0;
  bool chaotic_ok = true;

  for (int i = 0; i < 5000; ++i) {
    data.timestamp_ms = 5000ULL + i;
    bool is_online = (dist_burst(rng) == 0);
    WiFi.setMockStatus(is_online ? WL_CONNECTED : WL_CONNECTION_LOST);
    mockMqtt.setMockConnected(is_online);
    comm.tick();

    mockSerial.clearCapture();
    mockUdp.clearHistory();

    bool ok = comm.transmit(data);
    if (!ok) chaotic_ok = false;

    if (is_online) {
      chaotic_wifi++;
      if (mockUdp.getPacketCount() != 1 || !mockSerial.getCaptured().empty()) chaotic_ok = false;
    } else {
      chaotic_serial++;
      if (mockUdp.getPacketCount() != 0 || mockSerial.getCaptured().empty()) chaotic_ok = false;
    }
  }

  CHALL_ASSERT(chaotic_ok, "1.3.1 Asymmetric chaotic drop bursts handled with zero dropped frames");
  printf("     -> Chaotic Summary: Wi-Fi=%u (%.1f%%), Serial=%u (%.1f%%)\n",
         chaotic_wifi, (chaotic_wifi * 100.0f) / 5000.0f,
         chaotic_serial, (chaotic_serial * 100.0f) / 5000.0f);
  CHALL_ASSERT(chaotic_wifi > 350 && chaotic_wifi < 650, "1.3.2 Wi-Fi bursts aligned with expected stochastic ratio (~10%)");

  // 1.4: Reconnect Cooldown Throttling Under High-Frequency Flapping Storm
  printf("  -> 1.4: Testing reconnect flood throttling during network storms...\n");
  setSimulatedTime(true, 1000);
  WiFi.setMockStatus(WL_DISCONNECTED);
  comm.forceDisconnect();
  WiFi.reset();

  uint32_t sim_clock = 1000;
  // 10,000 ticks at 1ms resolution (10 seconds simulated time)
  for (int i = 0; i < 10000; ++i) {
    sim_clock += 1;
    setSimulatedMillis(sim_clock);
    comm.tick();
  }

  // Over 10 seconds with 3000ms reconnect interval, WiFi.begin should only be called ~3-4 times
  printf("     -> WiFi.begin() called %d times over 10,000 disconnected ticks (3000ms interval)\n", WiFi.beginCount);
  CHALL_ASSERT(WiFi.beginCount >= 3 && WiFi.beginCount <= 4, "1.4.1 WiFi.begin() strictly throttled to 3000ms interval");
  CHALL_ASSERT(WiFi.beginCount < 10, "1.4.2 Zero CPU/radio flood occurred during 10,000 tick storm");
  setSimulatedTime(false);

  // 1.5: State Machine Synchronization Invariant Audit
  CHALL_ASSERT(comm.getState() == COMM_STATE_DISCONNECTED, "1.5.1 State machine correctly sits in COMM_STATE_DISCONNECTED");
  comm.reconnect();
  CHALL_ASSERT(comm.getState() == COMM_STATE_CONNECTED, "1.5.2 Programmatic reconnect transitions cleanly to COMM_STATE_CONNECTED");
  CHALL_ASSERT(comm.isPrimaryTransportActive(), "1.5.3 Primary transport active after reconnect");
}

// =============================================================================
// SUITE 2: Comprehensive Socket Send Failures & UDP Packet Drop Injection
// =============================================================================
void run_suite_2_socket_failures_and_drops() {
  printf("\n================================================================================\n");
  printf(" [SUITE 2] Comprehensive Socket Send Failures & UDP Packet Drop Injection       \n");
  printf("================================================================================\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Socket_Stress_AP";
  cfg.mqtt_topic = "econ/telemetry/socket_stress";
  comm.begin(cfg);

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "cam_socket_01";
  data.zone_id = "zone_socket";
  data.person_detected = true;
  data.confidence = 0.91f;
  data.person_count = 1;

  mockSerial.setCapture(true, false);

  // 2.1: beginPacket() Failure Injection (500 Invocations)
  printf("  -> 2.1: Testing beginPacket() failure injection across 500 calls...\n");
  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(false); // isolate UDP
  mockUdp._fail_all_sends = true;   // forces beginPacket to return 0

  uint32_t pre_failovers = comm.getFailoverCount();
  uint32_t pre_fallback = comm.getFallbackTransmissions();
  bool begin_fail_ok = true;

  for (int i = 0; i < 500; ++i) {
    data.timestamp_ms = 10000ULL + i;
    mockSerial.clearCapture();
    bool res = comm.transmit(data);
    if (!res || mockSerial.getCaptured().empty()) {
      begin_fail_ok = false;
    }
  }

  CHALL_ASSERT(begin_fail_ok, "2.1.1 All 500 beginPacket() failures recovered instantaneously via Serial fallback");
  CHALL_ASSERT_EQ(500, (int)(comm.getFailoverCount() - pre_failovers), "2.1.2 Failover counter incremented exactly 500 times");
  CHALL_ASSERT_EQ(500, (int)(comm.getFallbackTransmissions() - pre_fallback), "2.1.3 Fallback transmission counter incremented 500 times");

  // 2.2: Partial write / truncation simulation
  mockUdp._fail_all_sends = false;

  // 2.3: endPacket() Failure Injection (500 Invocations)
  printf("  -> 2.3: Testing endPacket() failure injection across 500 calls...\n");
  pre_failovers = comm.getFailoverCount();
  pre_fallback = comm.getFallbackTransmissions();
  bool end_fail_ok = true;

  for (int i = 0; i < 500; ++i) {
    data.timestamp_ms = 20000ULL + i;
    mockUdp.failNextSend = true; // causes endPacket to fail
    mockSerial.clearCapture();
    bool res = comm.transmit(data);
    if (!res || mockSerial.getCaptured().empty()) {
      end_fail_ok = false;
    }
  }

  CHALL_ASSERT(end_fail_ok, "2.3.1 All 500 endPacket() failures recovered instantaneously via Serial fallback");
  CHALL_ASSERT_EQ(500, (int)(comm.getFailoverCount() - pre_failovers), "2.3.2 Failover counter incremented 500 times for endPacket failures");
  CHALL_ASSERT_EQ(500, (int)(comm.getFallbackTransmissions() - pre_fallback), "2.3.3 Fallback transmission counter incremented 500 times");

  // 2.4: Random Probabilistic UDP Drop Rates (25%, 50%, 75%, 99% across 4,000 packets)
  printf("  -> 2.4: Testing random UDP drop rates (25%%, 50%%, 75%%, 99%% across 4,000 packets)...\n");
  std::mt19937 drop_rng(42);
  std::uniform_int_distribution<int> dist_100(1, 100);

  const int drop_rates[] = {25, 50, 75, 99};
  for (int rate : drop_rates) {
    uint32_t init_succ = comm.getSuccessfulTransmissions();
    uint32_t init_fall = comm.getFallbackTransmissions();
    bool tier_all_pass = true;

    for (int i = 0; i < 1000; ++i) {
      data.timestamp_ms = 30000ULL + i;
      bool will_drop = (dist_100(drop_rng) <= rate);
      mockUdp.failNextSend = will_drop;

      if (!comm.transmit(data)) {
        tier_all_pass = false;
      }
    }

    uint32_t del_succ = comm.getSuccessfulTransmissions() - init_succ;
    uint32_t del_fall = comm.getFallbackTransmissions() - init_fall;

    char msg_buf[128];
    snprintf(msg_buf, sizeof(msg_buf), "2.4.%d 1,000 packets at %d%% drop rate delivered with 100%% total throughput", (rate / 25), rate);
    CHALL_ASSERT(tier_all_pass && (del_succ + del_fall == 1000), msg_buf);
  }

  // 2.5: Double Fault (UDP Fails + Serial Fallback Disabled)
  CommConfig no_fallback_cfg = cfg;
  no_fallback_cfg.enable_serial_fallback = false;
  DualModeComm no_fb_comm(mockUdp, mockMqtt, mockSerial);
  no_fb_comm.begin(no_fallback_cfg);

  mockUdp._fail_all_sends = true;
  mockSerial.clearCapture();
  bool double_fault_res = no_fb_comm.transmit(data);

  CHALL_ASSERT(!double_fault_res, "2.5.1 Double fault (UDP fails + Serial fallback disabled) cleanly returns false");
  CHALL_ASSERT(mockSerial.getCaptured().empty(), "2.5.2 Serial remains untouched when fallback disabled");
}

// =============================================================================
// SUITE 3: Serial Backpressure, Buffer Overruns & Telemetry Serialization Integrity
// =============================================================================
void run_suite_3_serial_and_serialization_stress() {
  printf("\n================================================================================\n");
  printf(" [SUITE 3] Serial Backpressure, Buffer Overruns & Serialization Integrity       \n");
  printf("================================================================================\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Serial_Stress_AP";
  cfg.mqtt_topic = "econ/telemetry/zone_serial";
  comm.begin(cfg);

  // Force offline to exercise Serial fallback path
  WiFi.setMockStatus(WL_DISCONNECTED);
  mockMqtt.setMockConnected(false);
  comm.tick();

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "cam_serial_node";
  data.zone_id = "zone_serial";

  // 3.1: High-Throughput Serial Fallback Burst (10,000 Continuous Frames)
  printf("  -> 3.1: Testing 10,000 high-throughput Serial fallback transmissions...\n");
  mockSerial.setCapture(false, false); // Avoid 2MB memory allocation in test string buffer

  auto t_start = std::chrono::high_resolution_clock::now();
  bool all_serial_tx_ok = true;

  for (int i = 0; i < 10000; ++i) {
    data.timestamp_ms = 1000000ULL + i;
    data.confidence = (i % 100) / 100.0f;
    data.person_detected = (data.confidence >= 0.50f);
    data.person_count = data.person_detected ? ((i % 4) + 1) : 0;

    if (!comm.transmit(data)) {
      all_serial_tx_ok = false;
      break;
    }
  }
  auto t_end = std::chrono::high_resolution_clock::now();
  double total_ms = std::chrono::duration<double, std::milli>(t_end - t_start).count();
  double avg_us = (total_ms * 1000.0) / 10000.0;

  printf("     -> 10,000 Serial transmits completed in %.2f ms (Average: %.3f us/tx)\n", total_ms, avg_us);
  CHALL_ASSERT(all_serial_tx_ok, "3.1.1 All 10,000 Serial transmissions succeeded without buffer overrun or drop");
  CHALL_ASSERT(avg_us < 25.0, "3.1.2 Serial transmit average latency < 25 us");

  // 3.2: Buffer Boundary Stress & Exact Size Canary Fuzzing (Lengths 0 .. 512 bytes)
  printf("  -> 3.2: Testing buffer boundary safety with memory canaries across sizes 0..512...\n");
  const size_t CANARY_SIZE = 64;
  const uint8_t CANARY_VAL = 0xEE;

  bool canaries_unbroken = true;
  bool return_bounds_valid = true;

  for (size_t test_len = 0; test_len <= 512; ++test_len) {
    std::vector<uint8_t> mem(CANARY_SIZE + test_len + CANARY_SIZE, CANARY_VAL);
    char* target_buf = (test_len > 0) ? (char*)(mem.data() + CANARY_SIZE) : nullptr;

    size_t written = serializeTrackingPayloadForSerial(data, "econ/telemetry/zone_serial", target_buf, test_len);

    // Verify pre-canary
    for (size_t c = 0; c < CANARY_SIZE; ++c) {
      if (mem[c] != CANARY_VAL) canaries_unbroken = false;
    }
    // Verify post-canary
    for (size_t c = 0; c < CANARY_SIZE; ++c) {
      if (mem[CANARY_SIZE + test_len + c] != CANARY_VAL) canaries_unbroken = false;
    }

    if (written > 0) {
      if (written >= test_len || target_buf[written] != '\0') return_bounds_valid = false;
    } else {
      if (test_len > 0 && target_buf[0] != '\0') return_bounds_valid = false;
    }
  }

  CHALL_ASSERT(canaries_unbroken, "3.2.1 64-byte pre- and post-memory canaries intact across all sizes 0..512");
  CHALL_ASSERT(return_bounds_valid, "3.2.2 Null termination and length return bounds strictly maintained");

  // 3.3: Pathological Input & Format String Fuzzing
  printf("  -> 3.3: Pathological input & format string fuzzing...\n");
  mockSerial.setCapture(true, false);

  PersonTrackingData pathData;
  initTrackingData(&pathData);
  pathData.sensor_id = "%s%s%n%x%d%p";
  pathData.zone_id = "%x%x%s%n";
  pathData.confidence = std::numeric_limits<float>::infinity(); // +Inf
  pathData.person_count = -999; // Negative count
  pathData.timestamp_ms = std::numeric_limits<uint64_t>::max(); // 64-bit max

  mockSerial.clearCapture();
  bool path_ok = comm.transmit(pathData);
  CHALL_ASSERT(path_ok, "3.3.1 Pathological input (+Inf, negative count, format strings, UINT64_MAX) handled safely");

  StaticJsonDocument<512> pathDoc;
  DeserializationError err = deserializeJson(pathDoc, mockSerial.getCaptured());
  CHALL_ASSERT(!err, "3.3.2 Serial output produced during pathological input is valid parseable JSON");
  CHALL_ASSERT_EQ("%s%s%n%x%d%p", pathDoc["sensor_id"].as<std::string>(), "3.3.3 Format strings preserved literally without injection");
  CHALL_ASSERT_EQ(0, pathDoc["person_count"].as<int>(), "3.3.4 Negative headcount safely clamped to 0");
  CHALL_ASSERT_FLOAT_NEAR(1.00f, pathDoc["confidence"].as<float>(), 0.01f, "3.3.5 +Infinity confidence clamped to 1.00");

  // 3.4: 2,000 Randomized Round-Trip Oracle Verifications
  printf("  -> 3.4: Executing 2,000 randomized round-trip oracle verifications...\n");
  std::mt19937_64 oracle_rng(12345);
  std::uniform_int_distribution<int> d_bool(0, 1);
  std::uniform_int_distribution<int> d_conf(0, 100);
  std::uniform_int_distribution<int> d_cnt(0, 20);
  std::uniform_int_distribution<uint64_t> d_ts(0ULL, 3000000000000ULL);

  const char* sensor_pool[] = {"cam_01", "cam_02", "ov7670_entry", "cam_desk_a", "cam_vip_room"};
  const char* zone_pool[] = {"zone_1", "zone_2", "zone_lobby", "zone_canteen", "zone_roof"};

  char serial_raw[512];
  char parsed_sensor[64];
  char parsed_zone[64];
  int oracle_mismatches = 0;

  for (int i = 0; i < 2000; ++i) {
    PersonTrackingData srcData;
    initTrackingData(&srcData);
    srcData.person_detected = (d_bool(oracle_rng) == 1);
    srcData.confidence = d_conf(oracle_rng) / 100.0f;
    srcData.person_count = srcData.person_detected ? d_cnt(oracle_rng) : 0;
    srcData.timestamp_ms = d_ts(oracle_rng);
    srcData.sensor_id = sensor_pool[i % 5];
    srcData.zone_id = zone_pool[i % 5];

    size_t w = serializeTrackingPayloadForSerial(srcData, "econ/telemetry/test", serial_raw, sizeof(serial_raw));
    if (w == 0) { oracle_mismatches++; continue; }

    PersonTrackingData dstData;
    memset(parsed_sensor, 0, sizeof(parsed_sensor));
    memset(parsed_zone, 0, sizeof(parsed_zone));

    bool parse_ok = deserializeTrackingPayload(serial_raw, w, &dstData, parsed_zone, sizeof(parsed_zone),
                                               parsed_sensor, sizeof(parsed_sensor));
    if (!parse_ok) { oracle_mismatches++; continue; }

    if (dstData.person_detected != srcData.person_detected ||
        std::abs(dstData.confidence - srcData.confidence) > 1e-2f ||
        dstData.person_count != srcData.person_count ||
        dstData.timestamp_ms != srcData.timestamp_ms ||
        strcmp(parsed_sensor, srcData.sensor_id) != 0 ||
        strcmp(parsed_zone, srcData.zone_id) != 0) {
      oracle_mismatches++;
    }
  }

  CHALL_ASSERT_EQ(0, oracle_mismatches, "3.4.1 2,000 randomized tracking payloads round-tripped with 0 mismatches (100% oracle match)");
}

// =============================================================================
// SUITE 4: Empirical Failover Latency Benchmarking (<100 µs Requirement)
// =============================================================================
void run_suite_4_failover_latency_benchmarks() {
  printf("\n================================================================================\n");
  printf(" [SUITE 4] Empirical Failover Latency Benchmarking (<100 µs Requirement)        \n");
  printf("================================================================================\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Latency_AP";
  comm.begin(cfg);

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "cam_bench";
  data.zone_id = "zone_bench";
  data.person_detected = true;
  data.confidence = 0.94f;
  data.person_count = 1;

  mockSerial.setCapture(false, false);

  // 4.1: 10,000 Failover Latency Benchmark Measurements
  printf("  -> 4.1: Benchmarking 10,000 failover events (Wi-Fi drop -> Serial frame emit)...\n");
  const int NUM_FAILOVERS = 10000;
  std::vector<double> failover_latencies_us;
  failover_latencies_us.reserve(NUM_FAILOVERS);

  for (int i = 0; i < NUM_FAILOVERS; ++i) {
    data.timestamp_ms = 500000ULL + i;

    // Put system in online mode
    WiFi.setMockStatus(WL_CONNECTED);
    mockMqtt.setMockConnected(true);
    comm.tick();

    // Trigger instant disconnect
    WiFi.setMockStatus(WL_DISCONNECTED);
    mockMqtt.setMockConnected(false);
    comm.tick();

    // Time the failover transmit
    auto t0 = std::chrono::high_resolution_clock::now();
    bool ok = comm.transmit(data);
    auto t1 = std::chrono::high_resolution_clock::now();

    if (!ok) {
      CHALL_ASSERT(false, "Failover transmit failed during latency loop");
      break;
    }

    double us = std::chrono::duration<double, std::micro>(t1 - t0).count();
    failover_latencies_us.push_back(us);
  }

  std::sort(failover_latencies_us.begin(), failover_latencies_us.end());

  double min_us = failover_latencies_us.front();
  double max_us = failover_latencies_us.back();
  double sum_us = std::accumulate(failover_latencies_us.begin(), failover_latencies_us.end(), 0.0);
  double mean_us = sum_us / NUM_FAILOVERS;
  double p50_us = failover_latencies_us[(size_t)(NUM_FAILOVERS * 0.50)];
  double p95_us = failover_latencies_us[(size_t)(NUM_FAILOVERS * 0.95)];
  double p99_us = failover_latencies_us[(size_t)(NUM_FAILOVERS * 0.99)];
  double p999_us = failover_latencies_us[(size_t)(NUM_FAILOVERS * 0.999)];

  printf("  --- Failover Latency Distribution (%d samples) ---\n", NUM_FAILOVERS);
  printf("    Min:         %8.3f us\n", min_us);
  printf("    Mean:        %8.3f us\n", mean_us);
  printf("    p50 (Med):   %8.3f us\n", p50_us);
  printf("    p95:         %8.3f us\n", p95_us);
  printf("    p99:         %8.3f us\n", p99_us);
  printf("    p99.9:       %8.3f us\n", p999_us);
  printf("    Max (Worst): %8.3f us\n", max_us);
  printf("  ---------------------------------------------------\n");

  CHALL_ASSERT(mean_us < 10.0, "4.1.1 Mean failover latency < 10 us (budget < 100 us)");
  CHALL_ASSERT(p99_us < 35.0, "4.1.2 99th percentile failover latency < 35 us");
  CHALL_ASSERT(max_us < 100.0, "4.1.3 Worst-case failover latency strictly < 100 us (Zero-Delay requirement)");

  // 4.2: Subsequent Telemetry Frame Continuity (100 frames immediately following failover)
  printf("  -> 4.2: Verifying continuity of 100 subsequent frames post-failover...\n");
  mockSerial.setCapture(true, false);
  mockSerial.clearCapture();

  bool post_failover_clean = true;
  for (int i = 0; i < 100; ++i) {
    data.timestamp_ms = 600000ULL + i;
    if (!comm.transmit(data)) post_failover_clean = false;
  }

  std::string burst_captured = mockSerial.getCaptured();
  int newline_count = std::count(burst_captured.begin(), burst_captured.end(), '\n');

  CHALL_ASSERT(post_failover_clean, "4.2.1 All 100 subsequent post-failover frames transmitted successfully");
  CHALL_ASSERT_EQ(100, newline_count, "4.2.2 Exactly 100 newline-terminated frames captured on Serial");
}

// =============================================================================
// SUITE 5: Integrated System Stress (Camera Person Detection + DualModeComm)
// =============================================================================
void run_suite_5_integrated_system_stress() {
  printf("\n================================================================================\n");
  printf(" [SUITE 5] Integrated System Stress (Camera Person Detection + DualModeComm)    \n");
  printf("================================================================================\n");

  CameraPersonDetector detector;
  CHALL_ASSERT(detector.init(), "5.1.1 CameraPersonDetector init() succeeds");
  detector.setZoneAndSensorId("zone_1", "esp32_cam_01");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);
  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Integrated_Stress_AP";
  comm.begin(cfg);

  mockSerial.setCapture(true, false);

  // 5.1: Continuous 500-Frame Inference Loop with Flapping Network Every 5 Frames
  printf("  -> 5.1: Running 500 integrated camera inference frames with flapping network every 5 frames...\n");
  
  uint32_t total_transmitted = 0;
  bool integrated_pass = true;

  for (int frame = 0; frame < 500; ++frame) {
    // Flap network every 5 frames
    bool online = ((frame / 5) % 2 == 0);
    WiFi.setMockStatus(online ? WL_CONNECTED : WL_DISCONNECTED);
    mockMqtt.setMockConnected(online);
    comm.tick();

    // Toggle simulated presence every 50 frames
    bool inject_person = ((frame / 50) % 2 == 1);
    detector.getDriver().setMockPersonDetected(inject_person);

    if (!detector.processFrame()) {
      integrated_pass = false;
      break;
    }

    mockSerial.clearCapture();
    mockUdp.clearHistory();

    detector.transmitTelemetry(comm);
    total_transmitted++;

    const PersonTrackingData& d = detector.getLatestData();
    if (inject_person && (frame % 50 >= 2)) {
      // After 2 debounce frames, person should be confirmed
      if (!d.person_detected || d.person_count != 1 || d.confidence < 0.65f) {
        integrated_pass = false;
      }
    } else if (!inject_person && (frame % 50 >= 2)) {
      if (d.person_detected || d.person_count != 0) {
        integrated_pass = false;
      }
    }

    // Verify packet routing
    if (online) {
      if (mockUdp.getPacketCount() != 1 || !mockSerial.getCaptured().empty()) integrated_pass = false;
    } else {
      if (mockUdp.getPacketCount() != 0 || mockSerial.getCaptured().empty()) integrated_pass = false;
    }
  }

  CHALL_ASSERT(integrated_pass, "5.1.2 500 integrated frames processed & transmitted with 100% presence & routing fidelity");
  CHALL_ASSERT_EQ(500, (int)total_transmitted, "5.1.3 Exactly 500 telemetry frames transmitted across flapping network");

  // 5.2: Stationary Occupant Continuous Tracking Across Network Drops
  printf("  -> 5.2: Verifying stationary occupant occupancy continuity across 50 frames during drop...\n");
  detector.getDriver().setMockPersonDetected(true);
  detector.processFrame(); // debounce 1
  detector.processFrame(); // debounce 2 -> detected
  CHALL_ASSERT(detector.isPersonDetected(), "5.2.1 Person detected prior to network drop");

  // Disconnect Wi-Fi and process 50 stationary frames
  WiFi.setMockStatus(WL_DISCONNECTED);
  mockMqtt.setMockConnected(false);
  comm.tick();

  bool stationary_held = true;
  for (int f = 0; f < 50; ++f) {
    detector.processFrame();
    if (!detector.isPersonDetected()) {
      stationary_held = false;
      break;
    }
    detector.transmitTelemetry(comm);
  }

  CHALL_ASSERT(stationary_held, "5.2.2 Stationary occupancy maintained 100% across all 50 frames during Wi-Fi outage");

  // 5.3: Main Loop Timing Budget Simulation (<150ms total cycle)
  printf("  -> 5.3: Simulating complete edge node main loop cycle timing...\n");
  auto t_loop_start = std::chrono::high_resolution_clock::now();
  
  detector.processFrame();
  comm.tick();
  detector.transmitTelemetry(comm);

  auto t_loop_end = std::chrono::high_resolution_clock::now();
  double loop_us = std::chrono::duration<double, std::micro>(t_loop_end - t_loop_start).count();
  
  printf("     -> Complete Main Loop step latency: %.3f us (%.3f ms)\n", loop_us, loop_us / 1000.0);
  CHALL_ASSERT(loop_us < 50000.0, "5.3.1 Main loop iteration executes in < 50 ms (well within 150ms 6.6 FPS budget)");
}

// =============================================================================
// Main Test Runner
// =============================================================================
int main() {
  printf("================================================================================\n");
  printf("   CHALLENGER 1 ADVERSARIAL STRESS TEST SUITE: MILESTONE 3 NETWORK & FAILOVER  \n");
  printf("================================================================================\n");

  run_suite_1_network_flapping_stress();
  run_suite_2_socket_failures_and_drops();
  run_suite_3_serial_and_serialization_stress();
  run_suite_4_failover_latency_benchmarks();
  run_suite_5_integrated_system_stress();

  printf("\n================================================================================\n");
  printf("                   CHALLENGER 1 STRESS TEST SUMMARY                             \n");
  printf("================================================================================\n");
  printf(" Total Checks Run : %d\n", g_challenger_total);
  printf(" Checks Passed    : %d\n", g_challenger_passed);
  printf(" Checks Failed    : %d\n", g_challenger_failed);
  printf(" Final Verdict    : %s\n", (g_challenger_failed == 0) ? "CONFIRM_CORRECTNESS (100% PASS)" : "REJECT (FAILURES FOUND)");
  printf("================================================================================\n\n");

  return (g_challenger_failed == 0) ? 0 : 1;
}
