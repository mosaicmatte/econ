import sys

with open("dashboard/src/HardwareInspector.jsx", "r") as f:
    lines = f.readlines()

out = []
for line in lines:
    if "const [networks, setNetworks] = useState([]);" in line:
        out.append(line)
        out.append("  const [scanning, setScanning] = useState(false);\n")
        continue

    if "if (scanMatch) {" in line:
        out.append(line)
        continue
        
    if "const scanDoneMatch = line.match(/\\[wifi\\] scan done/);" not in "".join(out) and "for (const line of lines) {" in line:
        out.append(line)
        out.append("          if (line.includes('[wifi] scan done')) setScanning(false);\n")
        continue

    if "onClick={() => { setNetworks([]); send('[wifi] scan'); }}" in line:
        out.append("              onClick={() => { setNetworks([]); setScanning(true); send('[wifi] scan'); }}\n")
        continue

    if ">Scan Networks<" in line or "> Scan Networks <" in line or ">Scan Networks\n" in line or "Scan Networks" in line:
        if "button" in "".join(lines) and "Scan Networks" in line and "<button" not in line and "onClick" not in line:
            out.append(line.replace("Scan Networks", "{scanning ? 'Scanning...' : 'Scan Networks'}"))
            continue

    out.append(line)

with open("dashboard/src/HardwareInspector.jsx", "w") as f:
    f.writelines(out)
