import paho.mqtt.client as mqtt
import time
import sys

MQTT_BROKER = "127.0.0.1"
MQTT_PORT = 1883
COMMAND_TOPIC = "econ/commands/+"

def on_connect(client, userdata, flags, rc):
    if rc == 0:
        print("\n[ESP32 EMULATOR] ✅ Connected to MQTT Broker at", MQTT_BROKER)
        client.subscribe(COMMAND_TOPIC)
        print(f"[ESP32 EMULATOR] 📡 Subscribed to {COMMAND_TOPIC} (Wildcard)")
        print("[ESP32 EMULATOR] ⏳ Waiting for Engine/YOLO commands...")
    else:
        print("[ESP32 EMULATOR] ❌ Connection failed with code", rc)

def on_message(client, userdata, msg):
    payload = msg.payload.decode('utf-8')
    print(f"\n[ESP32 EMULATOR] 📥 Received on {msg.topic}: {payload}")
    
    # Simulate the main.cpp command parsing
    if "LIGHTS_ON" in payload:
        print("   💡 [HARDWARE] -> RELAY CLICK: Turning Lights ON")
    elif "LIGHTS_OFF" in payload:
        print("   💡 [HARDWARE] -> RELAY CLICK: Turning Lights OFF")
        
    if "SETPOINT=" in payload:
        sp = payload.split("SETPOINT=")[1].split(";")[0]
        print(f"   ❄️  [HARDWARE] -> HVAC IR BLAST: Setting Temp to {sp}°C")

if __name__ == "__main__":
    print("===========================================")
    print("   ECON ESP32 IoT Node Emulator")
    print("===========================================")
    client = mqtt.Client(client_id="econ-esp32-emulator")
    client.on_connect = on_connect
    client.on_message = on_message
    
    try:
        client.connect(MQTT_BROKER, MQTT_PORT, 60)
        client.loop_forever()
    except Exception as e:
        print("Could not connect:", e)
        print("Is the Docker Compose engine and Mosquitto broker running?")
