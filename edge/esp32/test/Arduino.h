// Host stand-in for the Arduino core header. node_config.h includes <Arduino.h> for
// Serial, the fixed-width integer types and snprintf; on the host those come from the
// shim and the C library.
#pragma once
#include "arduino_shim.h"
