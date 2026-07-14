"""ECON Branch-A occupancy tracker: YOLOv8 + ByteTrack line-crossing counter.

Publishes real occupancy into the same edge wire contract the ESP32/Pico nodes use,
so a webcam pointed at a doorway becomes one more hardware node on the twin:

  * telemetry -> econ/telemetry/<topic>   {"zone", "occupancy", "source": "cv"}
  * liveness  -> econ/status/<topic>      retained online/offline + Last Will

Occupancy-only by design: the engine attributes it to source "cv" and never lets it
pin zone physics (that is reserved for genuinely measured temperatures).

Run on the laptop webcam:   python3 yolo_tracker.py --source 0
Run on demo footage:        python3 yolo_tracker.py --source people-detection.mp4
Headless (no cv2 window):   python3 yolo_tracker.py --source 0 --headless
"""

import argparse
import json
import logging
import time

import cv2
from ultralytics import YOLO
import supervision as sv
import paho.mqtt.client as mqtt

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("YOLO-Tracker")

parser = argparse.ArgumentParser(description="ECON CV occupancy node (YOLO + ByteTrack)")
parser.add_argument("--source", default="people-detection.mp4",
                    help="video file path, or a webcam index like 0")
parser.add_argument("--zone", default="Level 4", help="zone label sent as 'zone'")
parser.add_argument("--topic", default="zone_1", help="MQTT topic suffix for this node")
parser.add_argument("--broker", default="127.0.0.1")
parser.add_argument("--port", type=int, default=1883)
parser.add_argument("--model", default="yolov8n.pt", help="ultralytics weights (auto-downloads)")
parser.add_argument("--device", default="mps",
                    help="inference device: mps (Apple GPU), cuda, or cpu")
parser.add_argument("--line", default="50,300,550,300",
                    help="counting line as x1,y1,x2,y2 in frame pixels (crossing = in/out)")
parser.add_argument("--headless", action="store_true", help="no preview window (servers/SSH)")
args = parser.parse_args()

TELEMETRY_TOPIC = f"econ/telemetry/{args.topic}"
STATUS_TOPIC = f"econ/status/{args.topic}"
HEARTBEAT_SEC = 5.0  # re-publish period so the twin's freshness tracking sees us alive


def make_mqtt_client():
    cid = f"econ-cv-{args.topic}"
    try:  # paho-mqtt 2.x renamed the constructor contract
        client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION1, client_id=cid)
    except AttributeError:
        client = mqtt.Client(client_id=cid)
    client.will_set(STATUS_TOPIC, "offline", retain=True)

    def on_connect(c, *rest):
        c.publish(STATUS_TOPIC, "online", retain=True)
        logger.info("Connected to MQTT broker at %s:%d as %s", args.broker, args.port, cid)

    client.on_connect = on_connect
    client.reconnect_delay_set(min_delay=1, max_delay=10)
    try:
        client.connect_async(args.broker, args.port, 60)
        client.loop_start()
    except Exception as e:
        logger.error("MQTT unavailable (%s); running locally without publishing.", e)
    return client


def publish_occupancy(client, count: int):
    payload = json.dumps({"zone": args.zone, "occupancy": count, "source": "cv"})
    if client.is_connected():
        client.publish(TELEMETRY_TOPIC, payload)
        logger.info("Published to %s: %s", TELEMETRY_TOPIC, payload)


def run_tracking_pipeline(client):
    logger.info("Loading %s ...", args.model)
    model = YOLO(args.model)
    device = args.device

    tracker = sv.ByteTrack()
    x1, y1, x2, y2 = (int(v) for v in args.line.split(","))
    line_zone = sv.LineZone(start=sv.Point(x1, y1), end=sv.Point(x2, y2))

    box_annotator = sv.BoxAnnotator()
    label_annotator = sv.LabelAnnotator()
    line_zone_annotator = sv.LineZoneAnnotator()

    # "0" / "1" on the command line means a live camera index, anything else a file.
    source = int(args.source) if args.source.isdigit() else args.source
    cap = cv2.VideoCapture(source)
    if not cap.isOpened():
        logger.error("Failed to open video source: %r", source)
        return

    logger.info("Tracking %r -> zone %r (topic %s). Press 'q' to quit.",
                source, args.zone, args.topic)

    last_count = -1  # force an initial publish so the zone binds immediately
    last_publish = 0.0

    while True:
        ret, frame = cap.read()
        if not ret:
            break

        # classes=[0] restricts detection to 'person'. If the requested device isn't
        # available in this torch build (e.g. mps on Linux), fall back to CPU once.
        try:
            results = model(frame, classes=[0], device=device, verbose=False)[0]
        except Exception as e:
            if device != "cpu":
                logger.warning("Device %r failed (%s); falling back to cpu.", device, e)
                device = "cpu"
                continue
            raise

        detections = sv.Detections.from_ultralytics(results)
        detections = tracker.update_with_detections(detections)
        line_zone.trigger(detections=detections)

        # Net occupancy: crossings in minus crossings out, floored at zero.
        current_occupancy = max(0, line_zone.in_count - line_zone.out_count)

        # Publish on change, plus a heartbeat so staleness tracking sees a live node.
        now = time.time()
        if current_occupancy != last_count or now - last_publish >= HEARTBEAT_SEC:
            publish_occupancy(client, current_occupancy)
            last_count = current_occupancy
            last_publish = now

        if not args.headless:
            labels = [f"#{tid} Person" for tid in detections.tracker_id]
            frame = box_annotator.annotate(scene=frame, detections=detections)
            frame = label_annotator.annotate(scene=frame, detections=detections, labels=labels)
            frame = line_zone_annotator.annotate(frame, line_counter=line_zone)
            cv2.putText(frame, f"Occupancy: {current_occupancy}", (20, 50),
                        cv2.FONT_HERSHEY_SIMPLEX, 1, (0, 255, 0), 2)
            cv2.imshow("ECON CV Node - YOLO/ByteTrack Occupancy", frame)
            if cv2.waitKey(1) & 0xFF == ord("q"):
                break

    cap.release()
    if not args.headless:
        cv2.destroyAllWindows()


if __name__ == "__main__":
    mqtt_client = make_mqtt_client()
    try:
        run_tracking_pipeline(mqtt_client)
    finally:
        try:
            mqtt_client.publish(STATUS_TOPIC, "offline", retain=True).wait_for_publish(timeout=2)
        except Exception:
            pass
        mqtt_client.loop_stop()
        mqtt_client.disconnect()
