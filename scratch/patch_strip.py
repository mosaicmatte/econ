import sys

with open("edge/esp32/src/main.cpp", "r") as f:
    lines = f.readlines()

new_lines = []
for i, line in enumerate(lines):
    if line.strip() == "#ifndef USE_LUX":
        new_lines.append("#ifndef USE_STRIP\n")
        new_lines.append("  #define USE_STRIP 1\n")
        new_lines.append("#endif\n")
        new_lines.append("#if USE_STRIP\n")
        new_lines.append("  #ifndef STRIP_ADC_PIN\n")
        new_lines.append("    #define STRIP_ADC_PIN 35\n")
        new_lines.append("  #endif\n")
        new_lines.append("#endif\n")
        new_lines.append("\n")
    if line.strip() == "float readAcAmps() {":
        # Look for the end of readAcAmps()
        pass
        
    new_lines.append(line)

    if line.strip() == "return amps < 0.10 ? 0.0f : amps;" and lines[i+1].strip() == "}" and lines[i+2].strip() == "#endif":
        # Check if it's readAcAmps by scanning backwards a bit... actually, just find "#if USE_LUX" to insert before it
        pass

# safer injection:
out_lines = []
in_ac_amps = False
for i, line in enumerate(lines):
    if line.strip() == "#ifndef USE_LUX" and "#ifndef USE_STRIP" not in "".join(lines[max(0, i-10):i]):
        out_lines.append("#ifndef USE_STRIP\n")
        out_lines.append("  #define USE_STRIP 1\n")
        out_lines.append("#endif\n")
        out_lines.append("#if USE_STRIP\n")
        out_lines.append("  #ifndef STRIP_ADC_PIN\n")
        out_lines.append("    #define STRIP_ADC_PIN 35\n")
        out_lines.append("  #endif\n")
        out_lines.append("#endif\n")
    
    out_lines.append(line)
    
    if line.strip() == "#if USE_LUX" and "bool readLux" in lines[i+2]:
        out_lines.insert(-1, "#if USE_STRIP\n")
        out_lines.insert(-1, "float readStripAmps() {\n")
        out_lines.insert(-1, "  double sum = 0, sumSq = 0;\n")
        out_lines.insert(-1, "  int n = 0;\n")
        out_lines.insert(-1, "  unsigned long start = millis();\n")
        out_lines.insert(-1, "  while (millis() - start < 100) {\n")
        out_lines.insert(-1, "    int v = analogRead(STRIP_ADC_PIN);\n")
        out_lines.insert(-1, "    sum += v;\n")
        out_lines.insert(-1, "    sumSq += (double)v * v;\n")
        out_lines.insert(-1, "    n++;\n")
        out_lines.insert(-1, "  }\n")
        out_lines.insert(-1, "  if (n < 100) return -1;\n")
        out_lines.insert(-1, "  double mean = sum / n;\n")
        out_lines.insert(-1, "  double rmsCounts = sqrt(fmax(0.0, sumSq / n - mean * mean));\n")
        out_lines.insert(-1, "  const float dividerRatio = 10000.0 / (10000.0 + 10000.0);\n")
        out_lines.insert(-1, "  float amps = (float)(rmsCounts * (3.3 / 4095.0) / dividerRatio * gCfg.stripCalAPerV);\n")
        out_lines.insert(-1, "  return amps < 0.10 ? 0.0f : amps;\n")
        out_lines.insert(-1, "}\n")
        out_lines.insert(-1, "#endif\n\n")

    if line.strip() == "doc[\"plug\"] = plugOn ? \"ON\" : \"OFF\";" and "#endif" in lines[i+1]:
        # we'll inject after #endif
        pass
    if line.strip() == "#endif" and lines[i-1].strip() == "doc[\"plug\"] = plugOn ? \"ON\" : \"OFF\";":
        out_lines.append("#if USE_STRIP\n")
        out_lines.append("  float stripAmps = readStripAmps();\n")
        out_lines.append("  if (stripAmps >= 0) {\n")
        out_lines.append("    doc[\"stripW\"] = round(stripAmps * gCfg.plugMainsV * 10) / 10.0;\n")
        out_lines.append("  } else {\n")
        out_lines.append("    Serial.println(\"[strip] ADC window starved -> omitted\");\n")
        out_lines.append("  }\n")
        out_lines.append("#endif\n")
        
    if line.strip() == "supplyProbeReady = supplyProbe.getDeviceCount() > 0;" and "#endif" in lines[i+1]:
        pass
    if line.strip() == "#endif" and "supplyProbeReady" in lines[i-1]:
        out_lines.append("#if USE_STRIP\n")
        out_lines.append("  analogReadResolution(12);\n")
        out_lines.append("  Serial.printf(\"[strip] ACS712 on GPIO%d (cal %.1f A/V) — power strip metering\\n\", STRIP_ADC_PIN, (double)gCfg.stripCalAPerV);\n")
        out_lines.append("#endif\n")


with open("edge/esp32/src/main.cpp", "w") as f:
    f.writelines(out_lines)

