import paho.mqtt.client as mqtt
import json
import time

client = mqtt.Client()
client.connect("127.0.0.1", 1883, 60)
client.loop_start()

client.publish("econ/status/pico_1", "online", retain=True)
payload = {"zone": "Pico Lab", "occupancy": 1, "occupancy_2": 2, "source": "pico"}
client.publish("econ/telemetry/pico_1", json.dumps(payload))
time.sleep(1)
client.loop_stop()
