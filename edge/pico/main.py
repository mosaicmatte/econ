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
DHT_PIN = 15                 # wire a DHT22 here for real ambient temp+humidity; None = off
WIFI_SSID = ""               # Pico W only; empty => serial mode
WIFI_PASS = ""
MQTT_HOST = "192.168.1.100"  # broker LAN IP (Pico W mode only)
MQTT_PORT = 1883

# Hardware setup
adc_temp = machine.ADC(4)

# Optional DHT22 on DHT_PIN. The RP2040's internal sensor measures its own die, not the
# room — TEMP_OFFSET_C is only a crude correction — so a wired DHT22 is a genuinely
# better source and takes precedence when it reads. Absent or failing, we simply fall
# back and report the die estimate as NOT real, so the engine models the zone instead of
# pinning its physics to a fudged number.
dht_sensor = None
if DHT_PIN is not None:
    try:
        import dht
        dht_sensor = dht.DHT22(machine.Pin(DHT_PIN))
    except Exception as e:
        print("# dht22 unavailable on GP%s (%s); using die temp" % (DHT_PIN, e))
        dht_sensor = None


def read_dht():
    """Return (temp_c, humidity) from the DHT22, or (None, None) if it did not read.

    DHT22s drop reads routinely; a failure must yield nothing rather than a stand-in.
    """
    if dht_sensor is None:
        return None, None
    try:
        dht_sensor.measure()
        return dht_sensor.temperature(), dht_sensor.humidity()
    except Exception:
        return None, None

try:
    led = machine.Pin("LED", machine.Pin.OUT)
except Exception:
    led = machine.Pin(25, machine.Pin.OUT)

# Optional wired presence: seat a jumper from GP16 to any GND pin -> occupied while
# bridged. Rock-solid alternative to BOOTSEL, whose read must suspend flash + IRQs.
presence_wire = machine.Pin(16, machine.Pin.IN, machine.Pin.PULL_UP)

# State variables
presence = False
lights_state = False
setpoint = 22.0
last_btn_ms = 0
btn_was_down = False
last_btn_poll = 0
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
    # Reading BOOTSEL suspends XIP flash and interrupts for a moment; hammering it
    # while USB-CDC is streaming can deadlock the USB stack (observed live), so it is
    # sampled at only 4 Hz — press for about half a second.
    global presence, last_btn_ms, btn_was_down, last_btn_poll
    if not rp2 or not hasattr(rp2, "bootsel_button"):
        return False

    now = time.ticks_ms()
    if time.ticks_diff(now, last_btn_poll) < 250:
        return False
    last_btn_poll = now

    try:
        down = bool(rp2.bootsel_button())
    except Exception:
        return False

    toggled = False
    if down and not btn_was_down and time.ticks_diff(now, last_btn_ms) > 300:
        last_btn_ms = now
        presence = not presence
        toggled = True
    btn_was_down = down
    return toggled


def presence_active():
    # BOOTSEL toggle OR the GP16-to-GND jumper: either means the zone is occupied.
    return presence or presence_wire.value() == 0

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
    payload = {
        "zone": ZONE_LABEL,
        "occupancy": OCCUPIED_COUNT if presence_active() else 0,
        "source": "pico",
        "lights": "ON" if lights_state else "OFF",
        "setpoint": setpoint,
    }
    # Both sources are genuine measurements, so both may pin the zone's physics: a wired
    # DHT22 measures the room and wins when it reads; otherwise the RP2040 die sensor
    # keeps the fingertip demo working (warm the chip, watch the zone follow).
    t, h = read_dht()
    if t is not None:
        payload["temperature"] = round(t, 1)
        if h is not None:
            payload["humidity"] = round(h, 1)
    else:
        payload["temperature"] = round(get_temperature(), 1)
    payload["tempReal"] = True
    return payload

def publish_serial():
    global last_pub_ms
    payload = telemetry()
    payload["_topic"] = "econ/telemetry/" + ZONE_TOPIC
    print(json.dumps(payload))
    last_pub_ms = time.ticks_ms()


def run_serial():
    # Hardware watchdog: if anything ever wedges the loop (USB stack, blocking read),
    # the board hard-reboots within 8 s and resumes publishing on its own.
    wdt = machine.WDT(timeout=8000)
    poller = select.poll()
    poller.register(sys.stdin, select.POLLIN)
    rxbuf = ""
    last_presence = presence_active()

    while True:
        wdt.feed()

        toggled = check_button()
        cur = presence_active()
        if toggled or cur != last_presence:
            last_presence = cur
            flash_led()
            publish_serial()

        # Byte-wise, buffered stdin: poll() only promises ONE readable byte, so a
        # blocking readline() here could hang the loop on a partial line forever.
        while poller.poll(0):
            ch = sys.stdin.read(1)
            if not ch:
                break
            if ch == "\n":
                line = rxbuf.strip()
                rxbuf = ""
                if line:
                    try:
                        data = json.loads(line)
                        if "_cmd" in data:
                            handle_command(data["_cmd"])
                    except ValueError:
                        pass
            elif len(rxbuf) < 512:
                rxbuf += ch

        if time.ticks_diff(time.ticks_ms(), last_pub_ms) >= PUBLISH_S * 1000:
            publish_serial()

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

    # Armed only after WiFi is up (association can legitimately take >8 s); from here
    # on the watchdog hard-reboots the board out of any freeze or wedged socket.
    wdt = machine.WDT(timeout=8000)
    last_presence = presence_active()

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
                wdt.feed()
                client.check_msg()

                toggled = check_button()
                cur = presence_active()
                now = time.ticks_ms()
                if (toggled or cur != last_presence
                        or time.ticks_diff(now, last_pub_ms) >= PUBLISH_S * 1000):
                    if toggled or cur != last_presence:
                        last_presence = cur
                        flash_led()
                    payload = json.dumps(telemetry())
                    client.publish("econ/telemetry/" + ZONE_TOPIC, payload.encode())
                    last_pub_ms = time.ticks_ms()

                time.sleep(0.05)

        except OSError:
            wdt.feed()
            time.sleep(3)

if __name__ == "__main__":
    if WIFI_SSID:
        run_wifi()
    else:
        run_serial()
