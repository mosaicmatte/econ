import re

with open('edge/esp32/src/main.cpp', 'r') as f:
    c = f.read()

# Find the readStripAmps function and inject a print statement right before return
debug_print = """
  if (millis() % 5000 < 100) {
    Serial.printf("[strip-debug] mean=%.1f rmsCounts=%.1f amps=%.3f\\n", mean, rmsCounts, amps);
  }
  return amps < 0.10 ? 0.0f : amps;
"""
c = c.replace('return amps < 0.10 ? 0.0f : amps;', debug_print.strip())

with open('edge/esp32/src/main.cpp', 'w') as f:
    f.write(c)
