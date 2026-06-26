import re

with open('src/TelemetryPanel.jsx', 'r') as f:
    content = f.read()

# Add useState import
content = content.replace("import React, { useMemo } from 'react';", "import React, { useMemo, useState } from 'react';")

# Replace historicalData and currentData and add chartMode state
target_pattern = r"  // Generate some \"historical baseline\".*?const rcaData = getRCA\(faultZone\);"
replacement = """  const [activeChartMode, setActiveChartMode] = useState('office_dcv');

  const historicalData = useMemo(() => {
    const data = [];
    if (activeChartMode === 'office_dcv') {
      for (let i = 0; i < 300; i++) {
        const co2 = 400 + Math.random() * 3000;
        // Monotonic saturating curve
        const basePower = 200 + (Math.log(co2 - 300) * 50); 
        const actualPower = basePower + (Math.random() * 20 - 10);
        data.push({ x: Math.round(co2), y: Number(actualPower.toFixed(1)) });
      }
    } else if (activeChartMode === 'server_room') {
      for (let i = 0; i < 300; i++) {
        // Servers: IT Load (kW) vs Cooling Power (kW)
        const itLoad = 100 + Math.random() * 900;
        const coolingPower = itLoad * 0.35; // Approx PUE
        const actualPower = coolingPower + (Math.random() * 10 - 5);
        data.push({ x: Math.round(itLoad), y: Number(actualPower.toFixed(1)) });
      }
    }
    return data;
  }, [activeChartMode]);

  const currentData = useMemo(() => {
    // Make sure z.archetype is handled (fallback to 'office_dcv' if missing)
    const filteredZones = Object.values(simData.zones).filter(z => (z.archetype || 'office_dcv') === activeChartMode);
    
    return filteredZones.map((z, idx) => {
      const isFaulting = (activeScenario === 'fault' && z.id === faultTarget);
      
      if (activeChartMode === 'office_dcv') {
        let yPower = z.load * 20; 
        let co2 = 400 + (z.occupancy * 40);
        // Fault introduces high co2 and power deviation
        if (isFaulting) { co2 += 1000; yPower += 150; } 
        return { name: z.label, x: Math.round(co2), y: Number(yPower.toFixed(1)), isFaulting };
      } else {
        let itLoad = z.baseHeatGain * 10 + (z.load * 10);
        let yPower = z.load * 20; 
        // Fault introduces high cooling power despite constant IT load
        if (isFaulting) { yPower += 300; } 
        return { name: z.label, x: Math.round(itLoad), y: Number(yPower.toFixed(1)), isFaulting };
      }
    });
  }, [simData, activeChartMode, activeScenario, faultTarget]);

  // Insights Data logic
  const isFault = activeScenario === 'fault';
  const faultZoneId = isFault ? faultTarget : null;
  const faultZone = faultZoneId ? simData.zones[faultZoneId] : null;
  
  const unoccupiedWasting = Object.values(simData.zones).filter(z => z.occupancy === 0).slice(0, 1);
  const outOfBand = Object.values(simData.zones).filter(z => z.temp > z.setpoint + z.deadband && activeScenario !== 'fault');

  const getRCA = (zone) => {
    if (!zone) return { cause: 'Unknown error', blastRadius: 1, confidence: 0 };
    if (zone.type === 'server-room') {
      return { cause: 'CRAC unit compressor failure', blastRadius: 4, confidence: 96 };
    }
    if (zone.type === 'open-office') {
      return { cause: 'VAV box damper stuck closed', blastRadius: 2, confidence: 88 };
    }
    if (zone.type === 'perimeter') {
      return { cause: 'perimeter radiant heater stuck ON', blastRadius: 1, confidence: 85 };
    }
    return { cause: 'upstream chilled water valve failure', blastRadius: 3, confidence: 92 };
  };
  
  const rcaData = getRCA(faultZone);"""

content = re.sub(target_pattern, replacement, content, flags=re.DOTALL)

# Replace the Chart UI to add the tabs
ui_pattern = r"(<div style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: '300px' }}>\s*<div style={{ fontSize: '10px', fontWeight: 'bold', color: 'var\(--text-secondary\)', letterSpacing: '1px', marginBottom: '8px', display: 'flex', alignItems: 'center', gap: '6px' }}>\s*<Activity size=\{12\} color=\"var\(--accent-blue\)\" /> THERMODYNAMIC CHARACTERISTIC \(CO₂ vs POWER\)\s*</div>)"
ui_replacement = """<div style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: '350px' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
          <div style={{ fontSize: '10px', fontWeight: 'bold', color: 'var(--text-secondary)', letterSpacing: '1px', display: 'flex', alignItems: 'center', gap: '6px' }}>
            <Activity size={12} color="var(--accent-blue)" /> 
            {activeChartMode === 'office_dcv' ? 'OFFICE DCV (CO₂ vs COOLING kW)' : 'SERVER ROOM (IT kW vs COOLING kW)'}
          </div>
          <div style={{ display: 'flex', gap: '4px' }}>
            <button 
              onClick={() => setActiveChartMode('office_dcv')}
              style={{ background: activeChartMode === 'office_dcv' ? 'var(--accent-blue)' : 'var(--bg-obsidian)', color: activeChartMode === 'office_dcv' ? '#000' : 'var(--text-secondary)', border: '1px solid var(--border-glass)', fontSize: '9px', padding: '2px 6px', borderRadius: '4px', cursor: 'pointer', fontWeight: 'bold' }}
            >
              OFFICE
            </button>
            <button 
              onClick={() => setActiveChartMode('server_room')}
              style={{ background: activeChartMode === 'server_room' ? 'var(--accent-blue)' : 'var(--bg-obsidian)', color: activeChartMode === 'server_room' ? '#000' : 'var(--text-secondary)', border: '1px solid var(--border-glass)', fontSize: '9px', padding: '2px 6px', borderRadius: '4px', cursor: 'pointer', fontWeight: 'bold' }}
            >
              SERVER
            </button>
          </div>
        </div>"""

content = re.sub(ui_pattern, ui_replacement, content)

# Change XAxis/YAxis mapping
axis_pattern = r"(<XAxis type=\"number\" dataKey=\"x\" name=\"CO₂\" unit=\" ppm\" domain=\{\['auto', 'auto'\]\} stroke=\"var\(--text-secondary\)\" fontSize=\{10\} tickMargin=\{8\} />\s*<YAxis type=\"number\" dataKey=\"y\" name=\"Power\" unit=\" kW\" domain=\{\['auto', 'auto'\]\} stroke=\"var\(--text-secondary\)\" fontSize=\{10\} width=\{45\} />)"
axis_replacement = """<XAxis type="number" dataKey="x" name={activeChartMode === 'office_dcv' ? 'CO₂' : 'IT Load'} unit={activeChartMode === 'office_dcv' ? ' ppm' : ' kW'} domain={['auto', 'auto']} stroke="var(--text-secondary)" fontSize={10} tickMargin={8} />
              <YAxis type="number" dataKey="y" name="Cooling" unit=" kW" domain={['auto', 'auto']} stroke="var(--text-secondary)" fontSize={10} width={45} />"""
              
content = re.sub(axis_pattern, axis_replacement, content)

with open('src/TelemetryPanel.jsx', 'w') as f:
    f.write(content)
