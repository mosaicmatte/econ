import re

with open('dashboard/src/HardwareInspector.jsx', 'r') as f:
    content = f.read()

# Find the UsbTab component and replace the button part
usb_tab_original = '''  return (
    <div style={S.card}>
      <h2 style={{marginTop: 0}}>USB Serial Fallback & WiFi Config</h2>
      {!port ? (
        <button style={{...S.btn(true), padding: '8px 16px', fontSize: 14}} onClick={connectUsb}>Connect to ESP32 via USB</button>
      ) : ('''

usb_tab_new = '''  return (
    <div style={S.card}>
      <h2 style={{marginTop: 0}}>USB Serial Fallback & WiFi Config</h2>
      {!navigator.serial ? (
        <div style={S.warn}>
          <strong>Web Serial API is not supported in this browser.</strong><br/>
          Please use Chrome, Edge, or Opera to connect to the ESP32 via USB.
        </div>
      ) : !port ? (
        <button style={{...S.btn(true), padding: '8px 16px', fontSize: 14}} onClick={connectUsb}>Connect to ESP32 via USB</button>
      ) : ('''

content = content.replace(usb_tab_original, usb_tab_new)

with open('dashboard/src/HardwareInspector.jsx', 'w') as f:
    f.write(content)
