import sys

with open("edge/esp32/src/main.cpp", "r") as f:
    lines = f.readlines()

for i, line in enumerate(lines):
    if "if (n == 0) {" in line and "WiFi.scanNetworks()" in "".join(lines[i-5:i]):
        lines[i] = "      if (n == 0) {\n        Serial.println(\"[wifi] scanned: no networks found\");\n      } else if (n < 0) {\n        Serial.printf(\"[wifi] scan failed: %d\\n\", n);\n      } else {\n"
        break

with open("edge/esp32/src/main.cpp", "w") as f:
    f.writelines(lines)
