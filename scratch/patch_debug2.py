import re

with open('edge/esp32/src/main.cpp', 'r') as f:
    c = f.read()

# Replace the specific block of code
old_block = """
  if (millis() % 5000 < 100) {
    Serial.printf("[strip-debug] mean=%.1f rmsCounts=%.1f amps=%.3f\\n", mean, rmsCounts, amps);
  }
  return amps < 0.10 ? 0.0f : amps;
"""
new_block = """
  Serial.printf("[strip-debug] mean=%.1f rmsCounts=%.1f amps=%.3f\\n", mean, rmsCounts, amps);
  return amps < 0.10 ? 0.0f : amps;
"""
c = c.replace(old_block.strip(), new_block.strip())

with open('edge/esp32/src/main.cpp', 'w') as f:
    f.write(c)
