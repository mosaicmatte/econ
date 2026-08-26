#!/usr/bin/env bash
# Host-side test runner for ESP32 edge firmware off-target unit and integration tests.
# Runs node_config validation, M1 dual-mode comms, and M3 integration suites.
set -euo pipefail
cd "$(dirname "$0")/.."

JSON=.pio/libdeps/esp32dev/ArduinoJson/src
if [ ! -d "$JSON" ]; then
  echo "ArduinoJson not found at $JSON — checking alternative locations..." >&2
  FOUND_JSON=$(find . -path "*/ArduinoJson/src" -print -quit || true)
  if [ -n "$FOUND_JSON" ] && [ -d "$FOUND_JSON" ]; then
    JSON="$FOUND_JSON"
    echo "Found ArduinoJson at $JSON"
  else
    echo "ERROR: ArduinoJson not found at $JSON — run 'pio run -e esp32dev' once to fetch lib_deps." >&2
    exit 1
  fi
fi

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo "================================================================================"
echo "           STARTING ESP32 HOST OFF-TARGET UNIT TEST SUITE RUNNER                "
echo "================================================================================"
echo "ArduinoJson: $JSON"
echo ""

echo ">>> [1/3] Running Node Config Unit Tests..."
c++ -std=c++17 -Wall -Wextra -I "$JSON" -I src -I test test/host_config_test.cpp -o "$TMP_DIR/cfgtest"
"$TMP_DIR/cfgtest"

echo ""
echo ">>> [2/3] Running Milestone 1 Dual-Mode Communication Unit & Adversarial Tests..."
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

c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_adversarial_m1.cpp \
    -o "$TMP_DIR/advtest"
"$TMP_DIR/advtest"

echo ""
echo ">>> [3/3] Running Milestone 3 Main System Integration & Strict Isolation Tests..."
c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/ov7670_driver.cpp \
    src/camera/model_data.cpp \
    src/camera/person_detector.cpp \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_m3_integration.cpp \
    -o "$TMP_DIR/m3test"
"$TMP_DIR/m3test"

echo ""
echo ">>> [4/5] Running Milestone 3 Challenger 1 Adversarial Stress & Failover Tests..."
c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/ov7670_driver.cpp \
    src/camera/model_data.cpp \
    src/camera/person_detector.cpp \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_adversarial_m3_challenger1.cpp \
    -o "$TMP_DIR/m3adv1"
"$TMP_DIR/m3adv1"

echo ""
echo ">>> [5/5] Running Challenger 2 Full Adversarial Stress & Failover Test Suite..."
c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/ov7670_driver.cpp \
    src/camera/model_data.cpp \
    src/camera/person_detector.cpp \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_adversarial_challenger2_full.cpp \
    -o "$TMP_DIR/ch2full"
"$TMP_DIR/ch2full"

echo ""
echo ">>> [6/6] Running Milestone 2 ML Pipeline & Camera Driver Adversarial Stress Tests..."
c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/ov7670_driver.cpp \
    src/camera/model_data.cpp \
    src/camera/person_detector.cpp \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_adversarial_m2_ml.cpp \
    -o "$TMP_DIR/m2adv"
"$TMP_DIR/m2adv"

echo ""
echo "================================================================================"
echo "          ALL HOST TESTS COMPLETED AND PASSED WITH EXIT CODE 0                  "
echo "================================================================================"
exit 0

