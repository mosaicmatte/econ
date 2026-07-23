"""
A plain Pico has no radio, so this bridge gives it a network presence: it tails the
board's USB-serial JSON lines and republishes them to MQTT, forwards engine commands
back down the wire, and maintains the node's online/offline status (retained +
broker Last Will), making the Pico indistinguishable from a WiFi node to the engine.
"""

import glob
import json
import os
import sys
import time
import serial
import paho.mqtt.client as mqtt

# Configuration from environment variables
MQTT_BROKER = os.environ.get("MQTT_BROKER", "127.0.0.1")
MQTT_PORT = int(os.environ.get("MQTT_PORT", "1883"))
PICO_PORT = os.environ.get("PICO_PORT", "")
BAUD = int(os.environ.get("BAUD", "115200"))

def detect_port():
    if PICO_PORT:
        return PICO_PORT
    patterns = ["/dev/cu.usbmodem*", "/dev/tty.usbmodem*", "/dev/ttyACM*"]
    for pattern in patterns:
        matches = glob.glob(pattern)
        if matches:
            return matches[0]
    return None

def main():
    while True:
        port = detect_port()
        if not port:
            print("No Pico detected on /dev/cu.usbmodem*, /dev/tty.usbmodem*, or /dev/ttyACM*. Retrying in 3s...")
            time.sleep(3)
            continue
            
        print(f"Opening serial port {port} at {BAUD} baud...")
        try:
            # exclusive=True: a second bridge instance must fail loudly here instead of
            # silently splitting the byte stream with this one (which garbles every
            # line for both and flaps the node's online/offline status).
            ser = serial.Serial(port, BAUD, timeout=1.0, exclusive=True)
        except serial.SerialException as e:
            print(f"Failed to open port: {e}. Retrying in 3s...")
            time.sleep(3)
            continue

        print("Awaiting first valid JSON line from device with '_topic'...")
        suffix = None
        while True:
            try:
                line = ser.readline().decode(errors='replace').strip()
                if not line:
                    continue
                try:
                    data = json.loads(line)
                    if "_topic" in data:
                        suffix = data["_topic"].rsplit("/", 1)[-1]
                        print(f"Detected zone suffix: {suffix}")
                        break
                except ValueError:
                    print(f"[pico] {line}")
            except serial.SerialException:
                print("Device disconnected during sync.")
                break
            except KeyboardInterrupt:
                return

        if not suffix:
            ser.close()
            continue

        cid = f"bridge-pico-{suffix}"
        try:
            client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION1, client_id=cid)
        except AttributeError:
            client = mqtt.Client(client_id=cid)
        
        status_topic = f"econ/status/{suffix}"
        client.will_set(status_topic, "offline", retain=True)

        # Broker credentials. The shipped broker refuses anonymous clients (see
        # server/mosquitto/mosquitto.conf); run server/setup-mqtt-auth.sh to mint them.
        # Unset is only correct against the anonymous dev broker.
        _u = os.environ.get("MQTT_USER", "")
        if _u:
            client.username_pw_set(_u, os.environ.get("MQTT_PASS", ""))
        
        def safe_on_connect(*args, **kwargs):
            print(f"Connected to broker at {MQTT_BROKER}:{MQTT_PORT}")
            client.publish(status_topic, "online", retain=True)
            client.subscribe(f"econ/commands/{suffix}")
            print(f"Subscribed to econ/commands/{suffix}")
        
        client.on_connect = safe_on_connect
        
        def on_message(*args):
            # args can be (client, userdata, msg) or something else depending on version
            msg = args[-1]
            try:
                payload_str = msg.payload.decode()
                cmd = json.dumps({"_cmd": payload_str}) + "\n"
                ser.write(cmd.encode())
                print(f"Forwarded command: {payload_str}")
            except Exception as e:
                print(f"Error handling message: {e}")
                
        client.on_message = on_message
        
        connected = False
        while not connected:
            try:
                client.connect(MQTT_BROKER, MQTT_PORT)
                connected = True
            except Exception as e:
                print(f"Broker connection failed: {e}. Retrying in 3s...")
                time.sleep(3)
                
        client.loop_start()
        
        try:
            while True:
                line = ser.readline().decode(errors='replace').strip()
                if not line:
                    continue
                try:
                    data = json.loads(line)
                    if "_topic" in data:
                        topic = data.pop("_topic")
                        payload = json.dumps(data, separators=(',', ':'))
                        client.publish(topic, payload)
                        print(f"Published to {topic}: {payload}")
                except ValueError:
                    print(f"[pico] {line}")
        except serial.SerialException:
            print("Device unplugged.")
            _publish_offline(client, status_topic)
            ser.close()
            time.sleep(3)
        except KeyboardInterrupt:
            print("\nExiting...")
            _publish_offline(client, status_topic)
            ser.close()
            sys.exit(0)


def _publish_offline(client, status_topic):
    # A graceful disconnect suppresses the broker's Last Will, so the retained
    # "offline" must be flushed explicitly before tearing the connection down.
    try:
        client.publish(status_topic, "offline", retain=True).wait_for_publish(timeout=2)
    except Exception:
        pass
    client.loop_stop()
    client.disconnect()

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(0)
