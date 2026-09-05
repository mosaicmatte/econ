import re

with open('edge/esp32/src/main.cpp', 'r') as f:
    c = f.read()

# Fix redefinition of lastReconnectAttempt
# Look for the second one around line 905 and remove it
lines = c.split('\n')
for i, line in enumerate(lines):
    if 'unsigned long lastReconnectAttempt = 0;' in line:
        # Keep the first one, delete the second one
        pass

# Actually let's just use string replacement
# Remove the custom block we injected if it's there
c = c.replace('unsigned long lastReconnectAttempt = 0;\n\nvoid mqttConnect_custom()', 'void mqttConnect_custom()')

# Fix MQTT_USER / MQTT_PASS to just be nothing
c = c.replace('client.connect(CLIENT_ID, MQTT_USER, MQTT_PASS, STATUS_TOPIC, 1, true, "offline")', 'client.connect(CLIENT_ID, "", "", STATUS_TOPIC, 1, true, "offline")')

with open('edge/esp32/src/main.cpp', 'w') as f:
    f.write(c)

