with open('bridge.py', 'r') as f:
    c = f.read()

c = c.replace('mqtt_client.publish(match.group(1), match.group(2))', 'mqtt_client.publish(match.group(1), match.group(2))\n                        print("BRIDGE PUBLISHED!")')

with open('bridge.py', 'w') as f:
    f.write(c)
