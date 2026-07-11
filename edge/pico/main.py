"""
Raspberry Pi Pico edge-node firmware for the ECON digital twin.

Role:
Senses temperature via RP2040 internal sensor and presence via the BOOTSEL button.
Actuates an onboard LED as a simulated light.

Run modes:
A plain Pico has no radio, so serial mode over USB is the default.
A Pico W with WIFI_SSID configured runs standalone over WiFi.
"""

import json
import machine
import select
import sys
import time

try:
    import rp2
except ImportError:
    rp2 = None

# CONFIG block
ZONE_TOPIC = "pico_1"        # MQTT topic suffix
ZONE_LABEL = "Pico Lab"      # human label sent as "zone"
PUBLISH_S = 2                # telemetry period
OCCUPIED_COUNT = 2           # people reported while presence is toggled on
TEMP_OFFSET_C = 4.0          # RP2040 die runs ~4 C above ambient; subtracted out
WIFI_SSID = ""               # Pico W only; empty => serial mode
WIFI_PASS = ""
MQTT_HOST = "192.168.1.100"  # broker LAN IP (Pico W mode only)
MQTT_PORT = 1883

# Hardware setup
adc_temp = machine.ADC(4)

try:
    led = machine.Pin("LED", machine.Pin.OUT)
except Exception:
    led = machine.Pin(25, machine.Pin.OUT)

# State variables
presence = False
lights_state = False
setpoint = 22.0
last_btn_ms = 0
btn_was_down = False
last_pub_ms = 0

def get_temperature():
    # Average 8 samples
    total_volts = 0.0
    for _ in range(8):
        reading = adc_temp.read_u16()
        total_volts += (reading * 3.3) / 65535.0
    volts = total_volts / 8.0
    temp_c = 27 - (volts - 0.706) / 0.001721 - TEMP_OFFSET_C
    return temp_c

def check_button():
    # Toggle presence on the PRESS edge only (holding the button must not re-toggle).
    global presence, last_btn_ms, btn_was_down
    if not rp2 or not hasattr(rp2, "bootsel_button"):
        return False

    try:
        down = bool(rp2.bootsel_button())
    except Exception:
        return False

    toggled = False
    now = time.ticks_ms()
    if down and not btn_was_down and time.ticks_diff(now, last_btn_ms) > 300:
        last_btn_ms = now
        presence = not presence
        toggled = True
    btn_was_down = down
    return toggled

def flash_led():
    for _ in range(3):
        led.value(1)
        time.sleep(0.05)
        led.value(0)
        time.sleep(0.05)
    led.value(1 if lights_state else 0)

def handle_command(cmd_str):
    global lights_state, setpoint
    tokens = [t.strip() for t in cmd_str.split(";")]
    for t in tokens:
        if t == "LIGHTS_ON":
            lights_state = True
            led.value(1)
        elif t == "LIGHTS_OFF":
            lights_state = False
            led.value(0)
        elif t.startswith("SETPOINT="):
            try:
                setpoint = float(t.split("=")[1])
            except ValueError:
                pass
        elif t.startswith("HVAC_SET:"):
            try:
                setpoint = float(t.split(":")[1])
            except ValueError:
                pass

def telemetry():
    return {
        "zone": ZONE_LABEL,
        "occupancy": OCCUPIED_COUNT if presence else 0,
        "temperature": round(get_temperature(), 1),
        "source": "pico",
        "tempReal": True,
        "lights": "ON" if lights_state else "OFF",
        "setpoint": setpoint
    }

def run_serial():
    global last_pub_ms
    poller = select.poll()
    poller.register(sys.stdin, select.POLLIN)
    
    while True:
        if check_button():
            flash_led()
            payload = telemetry()
            payload["_topic"] = "econ/telemetry/" + ZONE_TOPIC
            print(json.dumps(payload))
            last_pub_ms = time.ticks_ms()
            
        if poller.poll(0):
            line = sys.stdin.readline().strip()
            if line:
                try:
                    data = json.loads(line)
                    if "_cmd" in data:
                        handle_command(data["_cmd"])
                except ValueError:
                    pass
                    
        now = time.ticks_ms()
        if time.ticks_diff(now, last_pub_ms) >= PUBLISH_S * 1000:
            payload = telemetry()
            payload["_topic"] = "econ/telemetry/" + ZONE_TOPIC
            print(json.dumps(payload))
            last_pub_ms = now
            
        time.sleep(0.05)

def run_wifi():
    try:
        import network
        from umqtt.simple import MQTTClient
    except ImportError:
        run_serial()
        return

    global last_pub_ms

    wlan = network.WLAN(network.STA_IF)
    wlan.active(True)
    wlan.connect(WIFI_SSID, WIFI_PASS)

    start = time.ticks_ms()
    while not wlan.isconnected():
        if time.ticks_diff(time.ticks_ms(), start) > 15000:
            wlan.active(False)
            run_serial()
            return
        time.sleep(0.1)

    while True:
        try:
            client_id = "econ-pico-" + ZONE_TOPIC
            client = MQTTClient(client_id, MQTT_HOST, port=MQTT_PORT)
            client.set_last_will("econ/status/" + ZONE_TOPIC, b"offline", retain=True)
            client.connect()
            client.publish("econ/status/" + ZONE_TOPIC, b"online", retain=True)
            
            def sub_cb(topic, msg):
                try:
                    handle_command(msg.decode())
                except Exception:
                    pass
                    
            client.set_callback(sub_cb)
            client.subscribe("econ/commands/" + ZONE_TOPIC)
            
            last_pub_ms = time.ticks_ms() - PUBLISH_S * 1000
            
            while True:
                client.check_msg()
                
                if check_button():
                    flash_led()
                    payload = json.dumps(telemetry())
                    client.publish("econ/telemetry/" + ZONE_TOPIC, payload.encode())
                    last_pub_ms = time.ticks_ms()
                    
                now = time.ticks_ms()
                if time.ticks_diff(now, last_pub_ms) >= PUBLISH_S * 1000:
                    payload = json.dumps(telemetry())
                    client.publish("econ/telemetry/" + ZONE_TOPIC, payload.encode())
                    last_pub_ms = now
                    
                time.sleep(0.05)
                
        except OSError:
            time.sleep(3)

if __name__ == "__main__":
    if WIFI_SSID:
        run_wifi()
    else:
        run_serial()
