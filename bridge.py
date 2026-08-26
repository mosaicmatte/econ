import serial
import time
import re
import paho.mqtt.client as mqtt

SERIAL_PORT = 'COM9'
BAUD_RATE = 115200
MQTT_BROKER = '127.0.0.1'
MQTT_PORT = 1883

print(f"Bat dau cau noi USB Serial -> MQTT (Offline Mode)")

def on_connect(client, userdata, flags, rc):
    print("MQTT Connected - Subscribing to commands...")
    client.subscribe("econ/commands/#")

def on_message(client, userdata, msg):
    try:
        topic = msg.topic
        payload = msg.payload.decode('utf-8')
        command_str = f"[mqtt] sub {topic} -> {payload}\n"
        print(f"WEB GHI XUONG: {command_str.strip()}")
        if userdata and 'ser' in userdata and userdata['ser'].is_open:
            userdata['ser'].write(command_str.encode('utf-8'))
    except Exception as e:
        print(f"Loi xu ly lenh Web: {e}")

mqtt_client = mqtt.Client()
mqtt_client.on_connect = on_connect
mqtt_client.on_message = on_message
mqtt_client.user_data_set({'ser': None})

try:
    mqtt_client.connect(MQTT_BROKER, MQTT_PORT, 60)
    mqtt_client.loop_start()
except Exception as e:
    print(f"Loi MQTT: {e}")
    exit(1)

regex = re.compile(r"\[mqtt\] pub (.+?) -> (\{.*\})")

try:
    while True:
        try:
            ser = serial.Serial(SERIAL_PORT, BAUD_RATE, timeout=1)
            mqtt_client.user_data_set({'ser': ser})
            print(f"Da mo cong {SERIAL_PORT}. Dang doc du lieu...")
            mqtt_client.publish("econ/status/zone_1", "online", retain=True)

            while True:
                if ser.in_waiting > 0:
                    line = ser.readline().decode('utf-8', errors='ignore').strip()
                    if not line: continue
                    
                    safe_line = line.encode('cp1252', errors='replace').decode('cp1252')
                    print(f"ESP32: {safe_line}")
                    match = regex.search(line)
                    if match:
                        mqtt_client.publish(match.group(1), match.group(2))
        except Exception as e:
            print(f"Loi USB: {e}. Thu lai sau 3 giay...")
            mqtt_client.user_data_set({'ser': None})
            time.sleep(3)
except KeyboardInterrupt:
    print("Dung chuong trinh...")
    mqtt_client.publish("econ/status/zone_1", "offline", retain=True)
    mqtt_client.loop_stop()
