import serial
import serial.tools.list_ports
import time
import re
import threading
import json
import paho.mqtt.client as mqtt

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
        if userdata and 'ports' in userdata:
            for ser in userdata['ports'].values():
                if ser.is_open:
                    ser.write(command_str.encode('utf-8'))
    except Exception as e:
        print(f"Loi xu ly lenh Web: {e}")

mqtt_client = mqtt.Client()
mqtt_client.on_connect = on_connect
mqtt_client.on_message = on_message
mqtt_client.user_data_set({'ports': {}})

try:
    mqtt_client.connect(MQTT_BROKER, MQTT_PORT, 60)
    mqtt_client.loop_start()
except Exception as e:
    print(f"Loi MQTT: {e}")
    exit(1)

regex = re.compile(r"\[mqtt\] pub (.+?) -> (\{.*\})")
active_ports = {}
lock = threading.Lock()

def handle_port(port):
    print(f"Da mo cong {port}. Dang doc du lieu...")
    try:
        ser = serial.Serial(port, BAUD_RATE, timeout=1)
        ser.dtr = False
        ser.rts = False
        
        with lock:
            ports = mqtt_client._userdata['ports']
            ports[port] = ser
        
        published_online = False
        
        while True:
            line = ser.readline().decode('utf-8', errors='ignore').strip()
            if not line: continue
            
            safe_line = line.encode('cp1252', errors='replace').decode('cp1252')
            
            # 1) Try parsing as direct JSON (Pico format)
            try:
                data = json.loads(safe_line)
                if "_topic" in data:
                    topic = data.pop("_topic")
                    if "source" not in data:
                        data["source"] = "pico"
                    mqtt_client.publish(topic, json.dumps(data))
                    
                    if not published_online:
                        suffix = topic.rsplit("/", 1)[-1]
                        mqtt_client.publish(f"econ/status/{suffix}", "online", retain=True)
                        published_online = True
                        
                    print(f"[{port}] BRIDGE PUBLISHED (Pico)!")
                    continue
            except Exception:
                pass
            
            # 2) Fallback to regex (ESP32 format)
            match = regex.search(line)
            if match:
                topic = match.group(1)
                mqtt_client.publish(topic, match.group(2))
                
                if not published_online:
                    suffix = topic.rsplit("/", 1)[-1]
                    mqtt_client.publish(f"econ/status/{suffix}", "online", retain=True)
                    published_online = True
                    
                print(f"[{port}] BRIDGE PUBLISHED (ESP32)!")
                
    except Exception as e:
        print(f"Loi USB tai {port}: {e}")
    finally:
        with lock:
            if port in mqtt_client._userdata['ports']:
                del mqtt_client._userdata['ports'][port]
            if port in active_ports:
                del active_ports[port]

try:
    while True:
        ports = serial.tools.list_ports.comports()
        for p in ports:
            # Match standard USB-Serial and USB-Modem patterns
            if ("usbserial" in p.device.lower() or "usbmodem" in p.device.lower() or "ttyacm" in p.device.lower() or "com" in p.device.lower()):
                if p.device not in active_ports:
                    active_ports[p.device] = True
                    t = threading.Thread(target=handle_port, args=(p.device,), daemon=True)
                    t.start()
        time.sleep(3)
except KeyboardInterrupt:
    print("Dung chuong trinh...")
    mqtt_client.loop_stop()

