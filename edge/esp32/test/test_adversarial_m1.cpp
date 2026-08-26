// -----------------------------------------------------------------------------
// test_adversarial_m1.cpp — Adversarial Stress Harness for Milestone 1
//
// Role: EMPIRICAL CHALLENGER
// Adversarial Scenarios:
//   1. Rapid Network State Flapping (Online <-> Offline alternating every cycle)
//   2. UDP Socket Send Failures (beginPacket & endPacket failure injection)
//   3. Extreme Load (100,000 continuous tick() and transmit() invocations)
//   4. Host Tick Latency Benchmarks & Detailed Percentile Distribution
//   5. Payload Fuzzing & Boundary Integrity
//   6. Reconnect Flood Protection & Hysteresis Under High Tick Rate
//   7. Multi-Transport Fault Combinations & Double Faults
//   8. Telemetry Deserialization Fuzzing & Robustness
// -----------------------------------------------------------------------------

#include "arduino_shim.h"
#include "WiFi.h"
#include "WiFiUdp.h"
#include "PubSubClient.h"
#include <ArduinoJson.h>

#include "tracking_payload.h"
#include "dual_mode_comm.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cmath>
#include <chrono>
#include <string>
#include <vector>
#include <algorithm>
#include <numeric>

static int g_failures = 0;
static int g_total_tests = 0;

static void check(bool condition, const char* description) {
  g_total_tests++;
  if (condition) {
    printf("  [PASS] %s\n", description);
  } else {
    printf("  [FAIL] %s\n", description);
    g_failures++;
  }
}

// -----------------------------------------------------------------------------
// Scenario 1: Rapid Network State Flapping
// -----------------------------------------------------------------------------
static void run_rapid_flapping_tests() {
  printf("\n=== [Scenario 1] Rapid Network State Flapping Stress Tests ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Flapping_Network_AP";
  cfg.wifi_pass = "Pass123";
  cfg.mqtt_topic = "econ/telemetry/zone_flapping";
  comm.begin(cfg);

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "cam_flap_01";
  data.zone_id = "zone_flap";
  data.person_detected = true;
  data.confidence = 0.92f;
  data.person_count = 1;

  mockSerial.setCapture(true, false);

  const int flap_cycles = 20000;
  bool flap_invariant_preserved = true;
  uint32_t expected_wifi_sends = 0;
  uint32_t expected_serial_sends = 0;

  printf("  Executing %d alternating online/offline cycles (40,000 state transitions)...\n", flap_cycles);

  for (int i = 0; i < flap_cycles; i++) {
    data.timestamp_ms = 1000ULL + i;

    // --- Phase A: Online ---
    WiFi.setMockStatus(WL_CONNECTED);
    mockMqtt.setMockConnected(true);
    comm.tick();

    if (!comm.isWifiConnected() || !comm.isPrimaryTransportActive() || comm.isSerialFallbackActive()) {
      flap_invariant_preserved = false;
    }

    mockSerial.clearCapture();
    mockUdp.clearHistory();
    bool tx_online = comm.transmit(data);
    if (!tx_online || mockUdp.getPacketCount() != 1 || !mockSerial.getCaptured().empty()) {
      flap_invariant_preserved = false;
    }
    expected_wifi_sends++;

    // --- Phase B: Offline ---
    WiFi.setMockStatus(WL_DISCONNECTED);
    mockMqtt.setMockConnected(false);
    comm.tick();

    if (comm.isWifiConnected() || comm.isPrimaryTransportActive() || !comm.isSerialFallbackActive()) {
      flap_invariant_preserved = false;
    }

    mockSerial.clearCapture();
    mockUdp.clearHistory();
    bool tx_offline = comm.transmit(data);
    if (!tx_offline || mockUdp.getPacketCount() != 0 || mockSerial.getCaptured().empty()) {
      flap_invariant_preserved = false;
    }
    expected_serial_sends++;
  }

  check(flap_invariant_preserved, "All 40,000 alternating transitions tracked state with 100% precision");
  check(comm.getSuccessfulTransmissions() == expected_wifi_sends, "Total Wi-Fi transmissions match expected count (20,000)");
  check(comm.getFallbackTransmissions() == expected_serial_sends, "Total Serial transmissions match expected count (20,000)");
  check(comm.getFailoverCount() >= (uint32_t)flap_cycles, "Failover counter correctly recorded offline state entries");

  // Asymmetric flapping test (Bursty connection drops)
  const int burst_cycles = 5000;
  bool burst_ok = true;
  for (int i = 0; i < burst_cycles; i++) {
    bool is_online = (i % 7 != 0); // Drop 1 every 7 frames
    WiFi.setMockStatus(is_online ? WL_CONNECTED : WL_CONNECTION_LOST);
    mockMqtt.setMockConnected(is_online);
    comm.tick();

    mockSerial.clearCapture();
    mockUdp.clearHistory();
    bool res = comm.transmit(data);
    if (!res) burst_ok = false;

    if (is_online) {
      if (mockUdp.getPacketCount() != 1 || !mockSerial.getCaptured().empty()) burst_ok = false;
    } else {
      if (mockUdp.getPacketCount() != 0 || mockSerial.getCaptured().empty()) burst_ok = false;
    }
  }
  check(burst_ok, "Asymmetric bursty packet loss handled without a single dropped telemetry frame");
}

// -----------------------------------------------------------------------------
// Scenario 2: Simulated UDP Socket Send Failures (Instant Fallback)
// -----------------------------------------------------------------------------
static void run_udp_socket_failure_tests() {
  printf("\n=== [Scenario 2] UDP Socket Failure & Instant Fallback Stress Tests ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Socket_Test_AP";
  cfg.mqtt_topic = "econ/telemetry/socket_test";
  comm.begin(cfg);

  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(false); // MQTT disconnected to isolate UDP socket behavior
  mockSerial.setCapture(true, false);
  comm.tick();

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "cam_fault_01";
  data.zone_id = "zone_fault";
  data.timestamp_ms = 500000ULL;
  data.person_detected = true;
  data.confidence = 0.89f;
  data.person_count = 2;

  // 1. beginPacket() Failure Injection (simulated via _fail_all_sends = true)
  mockUdp._fail_all_sends = true;
  mockSerial.clearCapture();
  mockUdp.clearHistory();
  uint32_t preFailovers = comm.getFailoverCount();
  uint32_t preFallback = comm.getFallbackTransmissions();

  bool res1 = comm.transmit(data);
  check(res1, "beginPacket() failure immediately falls back to Serial returning true");
  check(!mockSerial.getCaptured().empty(), "Serial output contains payload after beginPacket() failure");
  check(comm.getFailoverCount() == preFailovers + 1, "Failover counter incremented on beginPacket() failure");
  check(comm.getFallbackTransmissions() == preFallback + 1, "Fallback transmission count incremented");

  // 2. Fallback Serial Payload Integrity Validation after beginPacket failure
  StaticJsonDocument<512> doc;
  DeserializationError err = deserializeJson(doc, mockSerial.getCaptured());
  check(!err, "Serial fallback payload produced during socket failure is valid JSON");
  check(doc["sensor_id"] == "cam_fault_01", "JSON sensor_id matches");
  check(doc["zone_id"] == "zone_fault", "JSON zone_id matches");
  check(doc["person_count"].as<int>() == 2, "JSON person_count matches");
  check(doc["_topic"] == "econ/telemetry/socket_test", "JSON contains _topic field");

  // 3. endPacket() Failure Injection (simulated via failNextSend = true)
  mockUdp._fail_all_sends = false;
  mockUdp.failNextSend = true;
  mockSerial.clearCapture();
  mockUdp.clearHistory();
  preFailovers = comm.getFailoverCount();
  preFallback = comm.getFallbackTransmissions();

  bool res3 = comm.transmit(data);
  check(res3, "UDP endPacket() failure immediately falls back to Serial returning true");
  check(!mockSerial.getCaptured().empty(), "Serial output contains payload after endPacket() failure");
  check(comm.getFailoverCount() == preFailovers + 1, "Failover counter incremented on endPacket() failure");
  check(comm.getFallbackTransmissions() == preFallback + 1, "Fallback transmission count incremented");

  // 4. Zero-Delay Failover Execution Latency Measurement
  mockUdp._fail_all_sends = false;
  mockUdp.failNextSend = true; // inject single socket failure
  mockSerial.clearCapture();

  auto t0 = std::chrono::high_resolution_clock::now();
  bool instantRes = comm.transmit(data);
  auto t1 = std::chrono::high_resolution_clock::now();
  double failoverLatencyUs = std::chrono::duration<double, std::micro>(t1 - t0).count();

  check(instantRes, "Instant failover returns true");
  printf("    Instant socket failure fallback latency: %.3f us\n", failoverLatencyUs);
  check(failoverLatencyUs < 100.0, "Socket failure fallback completes in < 100 us (zero-delay instant failover)");

  // 5. Intermittent Socket Failure Stress (5,000 packets with 50% drop rate)
  mockUdp._fail_all_sends = false;
  mockSerial.clearCapture();

  uint32_t start_success = comm.getSuccessfulTransmissions();
  uint32_t start_fallback = comm.getFallbackTransmissions();
  const int interm_count = 5000;
  bool all_succeeded = true;

  for (int i = 0; i < interm_count; i++) {
    if (i % 2 == 1) {
      mockUdp.failNextSend = true; // Every odd packet fails UDP
    }
    bool ok = comm.transmit(data);
    if (!ok) all_succeeded = false;
  }

  uint32_t delta_success = comm.getSuccessfulTransmissions() - start_success;
  uint32_t delta_fallback = comm.getFallbackTransmissions() - start_fallback;

  check(all_succeeded, "All 5,000 intermittent socket failure calls returned true without data loss");
  check(delta_success + delta_fallback == (uint32_t)interm_count, "Sum of UDP + Serial fallback equals total 5,000 transmissions");
  check(delta_success == 2500, "Exactly 2,500 succeeded via UDP");
  check(delta_fallback == 2500, "Exactly 2,500 recovered via instant Serial fallback");
}

// -----------------------------------------------------------------------------
// Scenario 3: Non-Blocking Execution Under Extreme Load (100,000 Iterations)
// -----------------------------------------------------------------------------
static void run_extreme_load_tests() {
  printf("\n=== [Scenario 3] Extreme Load Stress Tests (100,000 Continuous Iterations) ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Load_AP";
  cfg.mqtt_topic = "econ/telemetry/load";
  comm.begin(cfg);

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "cam_load_01";
  data.zone_id = "zone_load";
  data.person_detected = true;
  data.confidence = 0.96f;
  data.person_count = 4;

  mockSerial.setCapture(false, false); // Disable capture buffer to avoid huge RAM allocation in test harness

  // 1. 100,000 Continuous tick() Invocations under varied states
  const int load_ticks = 100000;
  printf("  Running %d continuous tick() invocations across all comm states...\n", load_ticks);

  auto t_start = std::chrono::high_resolution_clock::now();
  for (int i = 0; i < load_ticks; i++) {
    // Cycle between states every 25k ticks
    if (i == 0)      WiFi.setMockStatus(WL_CONNECTED);
    if (i == 25000)  WiFi.setMockStatus(WL_DISCONNECTED);
    if (i == 50000)  WiFi.setMockStatus(WL_CONNECTION_LOST);
    if (i == 75000)  WiFi.setMockStatus(WL_CONNECTED);
    comm.tick();
  }
  auto t_end = std::chrono::high_resolution_clock::now();
  double total_tick_ms = std::chrono::duration<double, std::milli>(t_end - t_start).count();
  double avg_tick_ns = (total_tick_ms * 1e6) / load_ticks;

  printf("  Completed 100,000 tick() in %.2f ms (Average: %.2f ns/tick)\n", total_tick_ms, avg_tick_ns);
  check(total_tick_ms < 500.0, "100,000 ticks completed in < 500 ms (<5 us per tick)");

  // 2. 100,000 Continuous transmit() Invocations
  const int load_transmits = 100000;
  printf("  Running %d continuous transmit() calls (50k WiFi, 25k Offline, 25k Socket Faults)...\n", load_transmits);

  uint32_t init_success = comm.getSuccessfulTransmissions();
  uint32_t init_fallback = comm.getFallbackTransmissions();
  bool transmit_zero_loss = true;

  t_start = std::chrono::high_resolution_clock::now();
  for (int i = 0; i < load_transmits; i++) {
    data.timestamp_ms = 1000ULL + i;

    if (i < 50000) {
      // 50k Nominal WiFi
      WiFi.setMockStatus(WL_CONNECTED);
      mockUdp._fail_all_sends = false;
      mockUdp.failNextSend = false;
    } else if (i < 75000) {
      // 25k Offline Fallback
      WiFi.setMockStatus(WL_DISCONNECTED);
      mockUdp._fail_all_sends = false;
      mockUdp.failNextSend = false;
    } else {
      // 25k Connected with Socket Failures
      WiFi.setMockStatus(WL_CONNECTED);
      mockUdp._fail_all_sends = true;
    }

    if (!comm.transmit(data)) {
      transmit_zero_loss = false;
    }
  }
  t_end = std::chrono::high_resolution_clock::now();
  double total_tx_ms = std::chrono::duration<double, std::milli>(t_end - t_start).count();
  double avg_tx_us = (total_tx_ms * 1e3) / load_transmits;

  printf("  Completed 100,000 transmits in %.2f ms (Average: %.2f us/transmit)\n", total_tx_ms, avg_tx_us);
  check(transmit_zero_loss, "All 100,000 transmissions succeeded without a single unhandled error");

  uint32_t final_success = comm.getSuccessfulTransmissions() - init_success;
  uint32_t final_fallback = comm.getFallbackTransmissions() - init_fallback;

  check(final_success == 50000, "50,000 transmissions processed over Wi-Fi UDP");
  check(final_fallback == 50000, "50,000 transmissions processed over Serial fallback");
  check(final_success + final_fallback == (uint32_t)load_transmits, "Total accounted transmissions == 100,000");

  // 3. Memory Canary Stability Check
  uint8_t canary_block[32 + sizeof(DualModeComm) + 32];
  memset(canary_block, 0x5A, sizeof(canary_block));
  DualModeComm* stack_comm = new (canary_block + 32) DualModeComm(mockUdp, mockSerial);
  stack_comm->begin(cfg);

  for (int i = 0; i < 1000; i++) {
    stack_comm->tick();
    stack_comm->transmit(data);
  }
  stack_comm->~DualModeComm();

  bool lower_canary_ok = true, upper_canary_ok = true;
  for (int i = 0; i < 32; i++) {
    if (canary_block[i] != 0x5A) lower_canary_ok = false;
    if (canary_block[32 + sizeof(DualModeComm) + i] != 0x5A) upper_canary_ok = false;
  }
  check(lower_canary_ok && upper_canary_ok, "Zero memory boundary corruption or heap overrun detected in DualModeComm");
}

// -----------------------------------------------------------------------------
// Scenario 4: Host Tick Latency Benchmarking & Distribution Analysis
// -----------------------------------------------------------------------------
static void run_tick_latency_benchmarks() {
  printf("\n=== [Scenario 4] Host Tick Latency Benchmarks & Distribution Analysis ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Benchmark_AP";
  comm.begin(cfg);

  const int num_samples = 100000;
  std::vector<double> latencies_ns;
  latencies_ns.reserve(num_samples);

  // Warmup
  for (int i = 0; i < 1000; i++) comm.tick();

  // Benchmark Connected Mode Tick
  WiFi.setMockStatus(WL_CONNECTED);
  for (int i = 0; i < num_samples; i++) {
    auto t0 = std::chrono::high_resolution_clock::now();
    comm.tick();
    auto t1 = std::chrono::high_resolution_clock::now();
    double ns = (double)std::chrono::duration_cast<std::chrono::nanoseconds>(t1 - t0).count();
    latencies_ns.push_back(ns);
  }

  std::sort(latencies_ns.begin(), latencies_ns.end());

  double min_ns = latencies_ns.front();
  double max_ns = latencies_ns.back();
  double sum_ns = std::accumulate(latencies_ns.begin(), latencies_ns.end(), 0.0);
  double mean_ns = sum_ns / num_samples;
  double p50_ns = latencies_ns[(size_t)(num_samples * 0.50)];
  double p95_ns = latencies_ns[(size_t)(num_samples * 0.95)];
  double p99_ns = latencies_ns[(size_t)(num_samples * 0.99)];
  double p999_ns = latencies_ns[(size_t)(num_samples * 0.999)];

  printf("  --- DualModeComm::tick() Latency Distribution (%d samples) ---\n", num_samples);
  printf("    Min:        %8.2f ns (%.4f us)\n", min_ns, min_ns / 1000.0);
  printf("    Mean:       %8.2f ns (%.4f us)\n", mean_ns, mean_ns / 1000.0);
  printf("    p50 (Med):  %8.2f ns (%.4f us)\n", p50_ns, p50_ns / 1000.0);
  printf("    p95:        %8.2f ns (%.4f us)\n", p95_ns, p95_ns / 1000.0);
  printf("    p99:        %8.2f ns (%.4f us)\n", p99_ns, p99_ns / 1000.0);
  printf("    p99.9:      %8.2f ns (%.4f us)\n", p999_ns, p999_ns / 1000.0);
  printf("    Max (Worst):%8.2f ns (%.4f us)\n", max_ns, max_ns / 1000.0);

  // Assertions against budget requirements:
  // Required budget: < 200 us (< 0.2ms) per tick.
  check(max_ns < 200000.0, "Worst-case tick latency strictly < 200 us (<0.2ms budget)");
  check(p99_ns < 5000.0, "99th percentile tick latency < 5 us");
  check(mean_ns < 1000.0, "Mean tick latency < 1 us");

  // Benchmark Transmit Latencies (WiFi vs Serial vs Fallback)
  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "bench_01";
  data.zone_id = "zone_bench";
  data.person_detected = true;
  data.confidence = 0.95f;
  data.person_count = 1;

  mockSerial.setCapture(false, false);

  // WiFi transmit latency
  WiFi.setMockStatus(WL_CONNECTED);
  auto tx_t0 = std::chrono::high_resolution_clock::now();
  for (int i = 0; i < 10000; i++) comm.transmit(data);
  auto tx_t1 = std::chrono::high_resolution_clock::now();
  double wifi_tx_us = (std::chrono::duration<double, std::micro>(tx_t1 - tx_t0).count()) / 10000.0;
  printf("    WiFi transmit avg:     %.3f us\n", wifi_tx_us);
  check(wifi_tx_us < 50.0, "WiFi transmit avg latency < 50 us");

  // Serial fallback transmit latency
  WiFi.setMockStatus(WL_DISCONNECTED);
  tx_t0 = std::chrono::high_resolution_clock::now();
  for (int i = 0; i < 10000; i++) comm.transmit(data);
  tx_t1 = std::chrono::high_resolution_clock::now();
  double serial_tx_us = (std::chrono::duration<double, std::micro>(tx_t1 - tx_t0).count()) / 10000.0;
  printf("    Serial fallback avg:   %.3f us\n", serial_tx_us);
  check(serial_tx_us < 50.0, "Serial fallback avg latency < 50 us");
}

// -----------------------------------------------------------------------------
// Scenario 5: Payload Fuzzing & Boundary Integrity
// -----------------------------------------------------------------------------
static void run_payload_fuzzing_tests() {
  printf("\n=== [Scenario 5] Payload Fuzzing & Boundary Integrity Tests ===\n");

  char buf[512];

  // 1. Extreme float confidence fuzzing
  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "fuzz_cam";
  data.zone_id = "fuzz_zone";
  data.timestamp_ms = 12345ULL;
  data.person_detected = true;

  // Test NaN
  data.confidence = NAN;
  size_t len = serializeTrackingPayload(data, buf, sizeof(buf));
  check(len > 0, "NaN confidence serialized without crashing");

  // Test Positive Infinity
  data.confidence = INFINITY;
  len = serializeTrackingPayload(data, buf, sizeof(buf));
  check(len > 0, "+Infinity confidence clamped and serialized safely");
  StaticJsonDocument<256> doc;
  deserializeJson(doc, buf);
  check(std::abs(doc["confidence"].as<float>() - 1.0f) < 1e-2f, "+Infinity clamped to 1.00");

  // Test Negative Infinity
  data.confidence = -INFINITY;
  len = serializeTrackingPayload(data, buf, sizeof(buf));
  check(len > 0, "-Infinity confidence clamped and serialized safely");
  doc.clear();
  deserializeJson(doc, buf);
  check(std::abs(doc["confidence"].as<float>() - 0.0f) < 1e-2f, "-Infinity clamped to 0.00");

  // 2. Extreme Person Counts
  data.confidence = 0.5f;
  data.person_count = -2147483647; // INT_MIN
  len = serializeTrackingPayload(data, buf, sizeof(buf));
  doc.clear();
  deserializeJson(doc, buf);
  check(doc["person_count"].as<int>() == 0, "Negative INT_MIN count clamped to 0");

  data.person_count = 1000000;
  len = serializeTrackingPayload(data, buf, sizeof(buf));
  doc.clear();
  deserializeJson(doc, buf);
  check(doc["person_count"].as<int>() == 1000000, "Large person count formatted accurately");

  // 3. String Pathological Inputs
  data.person_count = 1;
  data.sensor_id = ""; // Empty string
  data.zone_id = "";
  len = serializeTrackingPayload(data, buf, sizeof(buf));
  check(len > 0, "Empty string sensor_id and zone_id handled safely");
  doc.clear();
  deserializeJson(doc, buf);
  check(doc["sensor_id"] == "", "Empty sensor_id preserved");
  check(doc["zone_id"] == "", "Empty zone_id preserved");

  // 4. Exact Buffer Capacity Stress
  data.sensor_id = "s";
  data.zone_id = "z";
  data.timestamp_ms = 0;
  data.confidence = 0.0f;
  data.person_count = 0;
  data.person_detected = false;

  char test_exact[256];
  size_t exact_len = serializeTrackingPayload(data, test_exact, sizeof(test_exact));
  check(exact_len > 0, "Exact length test payload generated");

  // Buffer with exact size required (including null terminator)
  std::vector<char> fit_buf(exact_len + 1, 'X');
  size_t fit_written = serializeTrackingPayload(data, fit_buf.data(), fit_buf.size());
  check(fit_written == exact_len, "Buffer with exact needed size succeeds");
  check(fit_buf[exact_len] == '\0', "Buffer correctly null terminated at boundary");

  // Buffer 1 byte too small
  std::vector<char> small_buf(exact_len, 'X');
  size_t small_written = serializeTrackingPayload(data, small_buf.data(), small_buf.size());
  check(small_written == 0, "Buffer 1 byte too small safely returns 0 and truncates");
  check(small_buf[0] == '\0', "Undersized buffer safely zeroed at index 0");
}

// -----------------------------------------------------------------------------
// Scenario 6: Reconnect Flood Protection & Hysteresis Stress Test
// -----------------------------------------------------------------------------
static void run_reconnect_hysteresis_tests() {
  printf("\n=== [Scenario 6] Reconnect Flood Protection & Hysteresis Tests ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Corporate_WiFi";
  cfg.reconnect_interval_ms = 5000; // 5 second cooldown
  comm.begin(cfg);

  setSimulatedTime(true, 1000);
  WiFi.setMockStatus(WL_DISCONNECTED);
  comm.forceDisconnect();
  WiFi.reset();

  uint32_t current_time_ms = 1000;

  // Advance time in 10ms increments for 60,000 ms (6,000 ticks) while disconnected
  for (int step = 0; step < 6000; step++) {
    current_time_ms += 10;
    setSimulatedMillis(current_time_ms);
    comm.tick();
  }

  // With 60 seconds of disconnected ticks and 5s interval, WiFi.begin should be called ~12 times
  // Not 6,000 times!
  printf("    WiFi.begin() called %d times over 60s of disconnected ticks (at 100Hz tick rate)\n", WiFi.beginCount);
  check(WiFi.beginCount >= 11 && WiFi.beginCount <= 13, "WiFi.begin() strictly throttled to 5000ms cooldown (no CPU/radio flooding)");
  check(WiFi.beginCount < 20, "No reconnection flood occurred under high tick rate");

  setSimulatedTime(false);
}

// -----------------------------------------------------------------------------
// Scenario 7: Multi-Transport Fault Combinations & Double Faults
// -----------------------------------------------------------------------------
static void run_multi_transport_fault_tests() {
  printf("\n=== [Scenario 7] Multi-Transport Fault Combinations & Double Faults ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "MultiFault_AP";
  cfg.mqtt_topic = "econ/telemetry/multifault";
  comm.begin(cfg);

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "cam_fault";
  data.zone_id = "zone_fault";
  data.person_detected = true;
  data.confidence = 0.90f;
  data.person_count = 1;

  // Case 1: UDP succeeds, MQTT fails
  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(false); // MQTT down
  mockUdp._fail_all_sends = false;
  mockSerial.clearCapture();
  mockUdp.clearHistory();

  bool res1 = comm.transmit(data);
  check(res1, "Transmit succeeds via UDP even if MQTT client is disconnected");
  check(mockUdp.getPacketCount() == 1, "UDP broadcast packet sent successfully");
  check(mockSerial.getCaptured().empty(), "Serial output remains silent");

  // Case 2: Double Fault (UDP fails AND Serial Fallback disabled)
  CommConfig noSerialCfg = cfg;
  noSerialCfg.enable_serial_fallback = false;
  DualModeComm noSerialComm(mockUdp, mockMqtt, mockSerial);
  noSerialComm.begin(noSerialCfg);

  mockUdp._fail_all_sends = true; // UDP fails
  mockSerial.clearCapture();

  bool res2 = noSerialComm.transmit(data);
  check(!res2, "Double fault (UDP fails + Serial disabled) returns false cleanly without hanging or crashing");
  check(mockSerial.getCaptured().empty(), "Serial remains untouched when fallback disabled");

  // Case 3: Calling transmitRaw with nullptr / 0 len
  bool res3 = comm.transmitRaw(nullptr, 100);
  check(!res3, "transmitRaw(nullptr) safely returns false");
  bool res4 = comm.transmitRaw("test", 0);
  check(!res4, "transmitRaw(len=0) safely returns false");
}

// -----------------------------------------------------------------------------
// Scenario 8: Telemetry Deserialization Fuzzing & Robustness
// -----------------------------------------------------------------------------
static void run_deserialization_fuzzing_tests() {
  printf("\n=== [Scenario 8] Telemetry Deserialization Fuzzing & Robustness ===\n");

  PersonTrackingData outData;
  char zoneBuf[32];
  char sensorBuf[32];

  // 1. Empty string
  bool r1 = deserializeTrackingPayload("", 0, &outData, zoneBuf, sizeof(zoneBuf), sensorBuf, sizeof(sensorBuf));
  check(!r1, "deserializeTrackingPayload(\"\") safely returns false");

  // 2. Null pointers
  bool r2 = deserializeTrackingPayload(nullptr, 10, &outData, zoneBuf, sizeof(zoneBuf), sensorBuf, sizeof(sensorBuf));
  check(!r2, "deserializeTrackingPayload(nullptr) safely returns false");

  bool r3 = deserializeTrackingPayload("{}", 2, nullptr, zoneBuf, sizeof(zoneBuf), sensorBuf, sizeof(sensorBuf));
  check(!r3, "deserializeTrackingPayload(null out_data) safely returns false");

  // 3. Truncated / malformed JSON
  const char* malformed1 = "{\"sensor_id\":\"esp32_cam_01\",\"zone_id\":";
  bool r4 = deserializeTrackingPayload(malformed1, strlen(malformed1), &outData, zoneBuf, sizeof(zoneBuf), sensorBuf, sizeof(sensorBuf));
  check(!r4, "Truncated JSON rejected safely");

  const char* malformed2 = "{not valid json at all}";
  bool r5 = deserializeTrackingPayload(malformed2, strlen(malformed2), &outData, zoneBuf, sizeof(zoneBuf), sensorBuf, sizeof(sensorBuf));
  check(!r5, "Malformed JSON syntax rejected safely");

  // 4. Binary random junk
  uint8_t junk[64];
  for (size_t i = 0; i < sizeof(junk); i++) junk[i] = (uint8_t)(i * 37 + 13);
  bool r6 = deserializeTrackingPayload((const char*)junk, sizeof(junk), &outData, zoneBuf, sizeof(zoneBuf), sensorBuf, sizeof(sensorBuf));
  check(!r6, "Random binary garbage safely rejected without crashing");

  // 5. Valid JSON with extra unknown fields
  const char* extraFieldsJson = "{\"sensor_id\":\"cam_extra\",\"zone_id\":\"zone_extra\",\"person_detected\":true,\"confidence\":0.88,\"person_count\":3,\"unknown_prop\":\"ignored\",\"extra_arr\":[1,2,3]}";
  bool r7 = deserializeTrackingPayload(extraFieldsJson, strlen(extraFieldsJson), &outData, zoneBuf, sizeof(zoneBuf), sensorBuf, sizeof(sensorBuf));
  check(r7, "Valid JSON with extra schema properties parsed correctly");
  check(outData.person_detected == true, "person_detected parsed correctly");
  check(outData.person_count == 3, "person_count parsed correctly");
  check(strcmp(sensorBuf, "cam_extra") == 0, "sensor_id parsed correctly");
}

// -----------------------------------------------------------------------------
// Main Adversarial Test Runner
// -----------------------------------------------------------------------------
int main() {
  printf("====================================================================\n");
  printf("  ADVERSARIAL STRESS HARNESS: MILESTONE 1 DUAL-MODE COMM & SCHEMA   \n");
  printf("====================================================================\n");

  run_rapid_flapping_tests();
  run_udp_socket_failure_tests();
  run_extreme_load_tests();
  run_tick_latency_benchmarks();
  run_payload_fuzzing_tests();
  run_reconnect_hysteresis_tests();
  run_multi_transport_fault_tests();
  run_deserialization_fuzzing_tests();

  printf("\n====================================================================\n");
  printf("Adversarial Summary: %d / %d tests passed\n", g_total_tests - g_failures, g_total_tests);
  printf("Adversarial Result: %s (%d failure%s)\n",
         g_failures == 0 ? "PASSED (100% SUCCESS — EMPIRICAL VERIFICATION COMPLETE)" : "FAILED",
         g_failures, g_failures == 1 ? "" : "s");
  printf("====================================================================\n");

  return g_failures ? 1 : 0;
}
