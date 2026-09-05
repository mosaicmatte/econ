import re

with open('dashboard/src/HardwareInspector.jsx', 'r') as f:
    content = f.read()

# Add 'usb' to tabs
content = content.replace("['devices', 'events', 'quality', 'forecast'].map", "['devices', 'events', 'quality', 'forecast', 'usb'].map")

usb_tab_code = '''
function UsbTab() {
  const [port, setPort] = useState(null);
  const [log, setLog] = useState([]);
  const [ssid, setSsid] = useState('');
  const [password, setPassword] = useState('');
  
  const connectUsb = async () => {
    try {
      const p = await navigator.serial.requestPort();
      await p.open({ baudRate: 115200 });
      setPort(p);
      readLoop(p);
    } catch (e) {
      console.error(e);
      alert("Failed to connect: " + e.message);
    }
  };

  const readLoop = async (p) => {
    const decoder = new TextDecoderStream();
    p.readable.pipeTo(decoder.writable);
    const reader = decoder.readable.getReader();
    let buffer = '';
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buffer += value;
      let lines = buffer.split('\\n');
      buffer = lines.pop();
      setLog(prev => [...prev, ...lines].slice(-50));
    }
  };

  const sendWifiConfig = async () => {
    if (!port) return;
    const encoder = new TextEncoder();
    const writer = port.writable.getWriter();
    await writer.write(encoder.encode(`[wifi] connect ${ssid} ${password}\\n`));
    writer.releaseLock();
    setSsid('');
    setPassword('');
  };

  return (
    <div style={S.card}>
      <h2 style={{marginTop: 0}}>USB Serial Fallback & WiFi Config</h2>
      {!port ? (
        <button style={{...S.btn(true), padding: '8px 16px', fontSize: 14}} onClick={connectUsb}>Connect to ESP32 via USB</button>
      ) : (
        <div>
          <div style={{display: 'flex', gap: 10, marginBottom: 16}}>
            <input placeholder="SSID" value={ssid} onChange={e => setSsid(e.target.value)} style={{padding: 8, background: '#1f2937', color: 'white', border: '1px solid #374151', borderRadius: 4}} />
            <input placeholder="Password" value={password} onChange={e => setPassword(e.target.value)} style={{padding: 8, background: '#1f2937', color: 'white', border: '1px solid #374151', borderRadius: 4}} />
            <button style={{...S.btn(true), padding: '8px 16px'}} onClick={sendWifiConfig}>Send WiFi Config</button>
          </div>
          <div style={{...S.pre, height: 300, overflowY: 'auto'}}>
            {log.map((l, i) => <div key={i}>{l}</div>)}
            {log.length === 0 && <div style={S.dim}>Waiting for serial data...</div>}
          </div>
        </div>
      )}
    </div>
  );
}
'''

# Add UsbTab component before HardwareInspector export
content = content.replace("export default function HardwareInspector() {", usb_tab_code + "\nexport default function HardwareInspector() {")

# Render UsbTab when tab === 'usb'
content = content.replace("{tab === 'forecast' && <ForecastTab />}", "{tab === 'forecast' && <ForecastTab />}\n      {tab === 'usb' && <UsbTab />}")

with open('dashboard/src/HardwareInspector.jsx', 'w') as f:
    f.write(content)
