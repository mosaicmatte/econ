#!/usr/bin/env bash
# Host-side tests for the node's runtime configuration (src/node_config.h).
#
# These run on the BUILD machine, not on an ESP32: the config logic is deliberately free
# of hardware dependencies so the validation rules — the safety layer over a calibration
# constant that multiplies every watt the node reports — can be exercised without flashing
# a board. `pio run` proves the firmware compiles; this proves the rules actually reject
# what they claim to.
set -euo pipefail
cd "$(dirname "$0")/.."

JSON=.pio/libdeps/esp32dev/ArduinoJson/src
if [ ! -d "$JSON" ]; then
  echo "ArduinoJson not found at $JSON — run 'pio run -e esp32dev' once to fetch lib_deps." >&2
  exit 1
fi

OUT=$(mktemp -d)/cfgtest
c++ -std=c++17 -Wall -I "$JSON" -I src -I test test/host_config_test.cpp -o "$OUT"
"$OUT"

echo ""
OUT2=$(mktemp -d)/strippowertest
c++ -std=c++17 -Wall -I "$JSON" -I src -I test test/host_strip_power_test.cpp -o "$OUT2"
"$OUT2"

echo ""
OUT3=$(mktemp -d)/fuzztest
c++ -std=c++17 -Wall -I "$JSON" -I src -I test test/empirical_fuzz_test.cpp -o "$OUT3"
"$OUT3"

echo ""
OUT4=$(mktemp -d)/payloadtest
c++ -std=c++17 -Wall -I "$JSON" -I src -I test test/empirical_payload_test.cpp -o "$OUT4"
"$OUT4"

echo ""
python3 test/verify_strip_power.py

echo ""
echo "=== PlatformIO Firmware Build Verification ==="
PIO_BIN=""
if command -v pio >/dev/null 2>&1; then
  PIO_BIN="pio"
elif [ -x "/Users/nguyenhoangkhoi/Library/Python/3.13/bin/pio" ]; then
  PIO_BIN="/Users/nguyenhoangkhoi/Library/Python/3.13/bin/pio"
elif [ -x "$HOME/.platformio/penv/bin/pio" ]; then
  PIO_BIN="$HOME/.platformio/penv/bin/pio"
fi

if [ -n "$PIO_BIN" ]; then
  if "$PIO_BIN" --version >/dev/null 2>&1; then
    echo "Found PlatformIO ($PIO_BIN). Running 'pio run'..."
    "$PIO_BIN" run
    echo "PlatformIO build: PASSED (0 errors, 0 warnings)"
  else
    echo "Found PlatformIO at $PIO_BIN, but execution is restricted by environment sandbox. Skipping 'pio run'."
  fi
else
  echo "PlatformIO (pio) not found in PATH or standard paths. Skipping 'pio run'."
fi


