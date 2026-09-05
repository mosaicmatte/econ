import sys

with open("edge/esp32/src/main.cpp", "r") as f:
    lines = f.readlines()

out = []
for line in lines:
    if "WiFi.disconnect();" in line:
        out.append("      WiFi.disconnect(true, true);\n")
        out.append("      delay(100);\n")
        continue
    out.append(line)

with open("edge/esp32/src/main.cpp", "w") as f:
    f.writelines(out)
