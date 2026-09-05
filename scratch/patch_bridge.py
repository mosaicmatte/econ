with open('bridge.py', 'r') as f:
    c = f.read()

# Remove the in_waiting check
c = c.replace('if ser.in_waiting > 0:', 'if True:')

# Prevent ESP32 reset on connect
c = c.replace('ser = serial.Serial(SERIAL_PORT, BAUD_RATE, timeout=1)', "ser = serial.Serial(SERIAL_PORT, BAUD_RATE, timeout=1)\n            ser.dtr = False\n            ser.rts = False")

with open('bridge.py', 'w') as f:
    f.write(c)
