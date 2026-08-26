// -----------------------------------------------------------------------------
// tracking_payload.cpp — BIM/Topology Tracking Payload Schema and Serializer
// -----------------------------------------------------------------------------
#include "tracking_payload.h"
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

#ifdef __cplusplus
extern "C" {
#endif

void initTrackingData(struct PersonTrackingData* data) {
    if (!data) return;
    data->person_detected = false;
    data->confidence = 0.0f;
    data->person_count = 0;
    data->timestamp_ms = 0;
    data->zone_id = "unknown_zone";
    data->sensor_id = "unknown_sensor";
    data->inference_time_ms = 0;
    data->fps = 0.0f;
    data->bboxes = NULL;
    data->bbox_count = 0;
}

bool validateTrackingData(const struct PersonTrackingData* data) {
    if (!data) return false;
    if (!data->zone_id || !data->sensor_id) return false;
    if (data->confidence < 0.0f || data->confidence > 1.0f) return false;
    if (data->person_count < 0) return false;
    return true;
}

size_t serializeTrackingPayloadPtr(const struct PersonTrackingData* data, char* buffer, size_t max_len) {
    if (!buffer || max_len == 0) {
        return 0;
    }

    if (!data) {
        buffer[0] = '\0';
        return 0;
    }

    // Clamp confidence to [0.0, 1.0]
    float conf = data->confidence;
    if (conf < 0.0f) conf = 0.0f;
    else if (conf > 1.0f) conf = 1.0f;

    // Clamp person_count to non-negative
    int count = data->person_count < 0 ? 0 : data->person_count;

    // Null-safe strings
    const char* sensor = data->sensor_id ? data->sensor_id : "unknown_sensor";
    const char* zone = data->zone_id ? data->zone_id : "unknown_zone";

    // Direct bounded formatting with zero heap allocations
    int written = snprintf(buffer, max_len,
        "{\"sensor_id\":\"%s\",\"zone_id\":\"%s\",\"timestamp_ms\":%llu,\"person_detected\":%s,\"confidence\":%.2f,\"person_count\":%d}",
        sensor,
        zone,
        (unsigned long long)data->timestamp_ms,
        data->person_detected ? "true" : "false",
        (double)conf,
        count
    );

    // Strict buffer boundary check
    if (written < 0 || (size_t)written >= max_len) {
        buffer[0] = '\0';
        return 0;
    }

    return (size_t)written;
}

size_t serializeTrackingPayloadForSerialPtr(const struct PersonTrackingData* data, const char* topic, char* buffer, size_t max_len) {
    if (!buffer || max_len == 0) {
        return 0;
    }

    if (!data) {
        buffer[0] = '\0';
        return 0;
    }

    if (!topic || topic[0] == '\0') {
        return serializeTrackingPayloadPtr(data, buffer, max_len);
    }

    // Clamp confidence to [0.0, 1.0]
    float conf = data->confidence;
    if (conf < 0.0f) conf = 0.0f;
    else if (conf > 1.0f) conf = 1.0f;

    int count = data->person_count < 0 ? 0 : data->person_count;
    const char* sensor = data->sensor_id ? data->sensor_id : "unknown_sensor";
    const char* zone = data->zone_id ? data->zone_id : "unknown_zone";

    int written = snprintf(buffer, max_len,
        "{\"sensor_id\":\"%s\",\"zone_id\":\"%s\",\"timestamp_ms\":%llu,\"person_detected\":%s,\"confidence\":%.2f,\"person_count\":%d,\"_topic\":\"%s\"}",
        sensor,
        zone,
        (unsigned long long)data->timestamp_ms,
        data->person_detected ? "true" : "false",
        (double)conf,
        count,
        topic
    );

    if (written < 0 || (size_t)written >= max_len) {
        buffer[0] = '\0';
        return 0;
    }

    return (size_t)written;
}

size_t serializeExtendedTrackingPayloadPtr(const struct PersonTrackingData* data, char* buffer, size_t max_len) {
    if (!buffer || max_len == 0) return 0;
    if (!data) {
        buffer[0] = '\0';
        return 0;
    }

    // If no extended fields present, use standard canonical serialization
    if (data->inference_time_ms == 0 && data->fps <= 0.0f && (!data->bboxes || data->bbox_count == 0)) {
        return serializeTrackingPayloadPtr(data, buffer, max_len);
    }

    float conf = data->confidence;
    if (conf < 0.0f) conf = 0.0f;
    else if (conf > 1.0f) conf = 1.0f;

    int count = data->person_count < 0 ? 0 : data->person_count;
    const char* sensor = data->sensor_id ? data->sensor_id : "unknown_sensor";
    const char* zone = data->zone_id ? data->zone_id : "unknown_zone";

    int offset = snprintf(buffer, max_len,
        "{\"sensor_id\":\"%s\",\"zone_id\":\"%s\",\"timestamp_ms\":%llu,\"person_detected\":%s,\"confidence\":%.2f,\"person_count\":%d",
        sensor,
        zone,
        (unsigned long long)data->timestamp_ms,
        data->person_detected ? "true" : "false",
        (double)conf,
        count
    );

    if (offset < 0 || (size_t)offset >= max_len) {
        buffer[0] = '\0';
        return 0;
    }

    if (data->inference_time_ms > 0) {
        int n = snprintf(buffer + offset, max_len - offset, ",\"inference_ms\":%u", (unsigned int)data->inference_time_ms);
        if (n < 0 || (size_t)(offset + n) >= max_len) { buffer[0] = '\0'; return 0; }
        offset += n;
    }

    if (data->fps > 0.0f) {
        int n = snprintf(buffer + offset, max_len - offset, ",\"fps\":%.1f", (double)data->fps);
        if (n < 0 || (size_t)(offset + n) >= max_len) { buffer[0] = '\0'; return 0; }
        offset += n;
    }

    if (data->bboxes && data->bbox_count > 0) {
        size_t bcount = data->bbox_count > TRACKING_PAYLOAD_MAX_BBOXES ? TRACKING_PAYLOAD_MAX_BBOXES : data->bbox_count;
        int n = snprintf(buffer + offset, max_len - offset, ",\"bboxes\":[");
        if (n < 0 || (size_t)(offset + n) >= max_len) { buffer[0] = '\0'; return 0; }
        offset += n;

        for (size_t i = 0; i < bcount; i++) {
            const struct TrackingBoundingBox& b = data->bboxes[i];
            int bn = snprintf(buffer + offset, max_len - offset,
                "%s{\"xmin\":%.2f,\"ymin\":%.2f,\"xmax\":%.2f,\"ymax\":%.2f,\"conf\":%.2f}",
                (i > 0 ? "," : ""),
                (double)b.xmin, (double)b.ymin, (double)b.xmax, (double)b.ymax, (double)b.confidence
            );
            if (bn < 0 || (size_t)(offset + bn) >= max_len) { buffer[0] = '\0'; return 0; }
            offset += bn;
        }

        n = snprintf(buffer + offset, max_len - offset, "]");
        if (n < 0 || (size_t)(offset + n) >= max_len) { buffer[0] = '\0'; return 0; }
        offset += n;
    }

    int n = snprintf(buffer + offset, max_len - offset, "}");
    if (n < 0 || (size_t)(offset + n) >= max_len) {
        buffer[0] = '\0';
        return 0;
    }
    offset += n;

    return (size_t)offset;
}

}

#include <ArduinoJson.h>

extern "C" {

bool deserializeTrackingPayload(
    const char* json_str,
    size_t len,
    struct PersonTrackingData* out_data,
    char* zone_buf,
    size_t zone_buf_len,
    char* sensor_buf,
    size_t sensor_buf_len
) {
    if (!json_str || len == 0 || !out_data) return false;

    initTrackingData(out_data);

    StaticJsonDocument<512> doc;
    DeserializationError err = deserializeJson(doc, json_str);
    if (err) return false;

    if (doc.containsKey("person_detected")) {
        out_data->person_detected = doc["person_detected"].as<bool>();
    }
    if (doc.containsKey("confidence")) {
        out_data->confidence = doc["confidence"].as<float>();
    }
    if (doc.containsKey("person_count")) {
        out_data->person_count = doc["person_count"].as<int>();
    }
    if (doc.containsKey("timestamp_ms")) {
        out_data->timestamp_ms = doc["timestamp_ms"].as<uint64_t>();
    }
    if (doc.containsKey("sensor_id") && sensor_buf && sensor_buf_len > 0) {
        const char* s = doc["sensor_id"].as<const char*>();
        if (s) {
            strncpy(sensor_buf, s, sensor_buf_len - 1);
            sensor_buf[sensor_buf_len - 1] = '\0';
            out_data->sensor_id = sensor_buf;
        }
    }
    if (doc.containsKey("zone_id") && zone_buf && zone_buf_len > 0) {
        const char* z = doc["zone_id"].as<const char*>();
        if (z) {
            strncpy(zone_buf, z, zone_buf_len - 1);
            zone_buf[zone_buf_len - 1] = '\0';
            out_data->zone_id = zone_buf;
        }
    }
    if (doc.containsKey("inference_ms")) {
        out_data->inference_time_ms = doc["inference_ms"].as<uint16_t>();
    }
    if (doc.containsKey("fps")) {
        out_data->fps = doc["fps"].as<float>();
    }

    return true;
}

#ifdef __cplusplus
}
#endif
