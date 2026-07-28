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
