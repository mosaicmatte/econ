// -----------------------------------------------------------------------------
// tracking_payload.h — BIM/Topology Tracking Payload Schema and Serializer
//
// Role in ECON Edge Node:
//   * Formats OV7670 camera person detection and occupancy telemetry.
//   * Generates compact, zero-heap JSON for Wi-Fi UDP broadcast (:4210)
//     and USB Serial fallback (115200 baud).
//   * Feeds the Go backend 2R1C thermal simulation and BIM digital twin.
// -----------------------------------------------------------------------------
#pragma once

#include <stddef.h>
#include <stdint.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

#define TRACKING_PAYLOAD_MAX_ID_LEN          32
#define TRACKING_PAYLOAD_BUFFER_SIZE         256
#define TRACKING_PAYLOAD_EXT_BUFFER_SIZE     512
#define TRACKING_PAYLOAD_MAX_BBOXES          4

/**
 * @brief Normalized Bounding Box metadata for spatial tracking extensions.
 * Coordinates are normalized to [0.0, 1.0] relative to frame dimensions.
 */
struct TrackingBoundingBox {
    float xmin;        // Left edge [0.0 .. 1.0]
    float ymin;        // Top edge [0.0 .. 1.0]
    float xmax;        // Right edge [0.0 .. 1.0]
    float ymax;        // Bottom edge [0.0 .. 1.0]
    float confidence;  // Detection confidence [0.0 .. 1.0]
    int16_t class_id;  // Class identifier (default 0 = person)
};

/**
 * @brief Core Person Tracking Data structure.
 * Fits within 48 bytes on 32-bit architecture (ESP32 Xtensa LX6).
 */
struct PersonTrackingData {
    bool person_detected;              // True if at least one person is detected
    float confidence;                  // Primary detection score [0.00 .. 1.00]
    int person_count;                  // Headcount estimate (>= 0)
    uint64_t timestamp_ms;             // Unix timestamp (ms) or millis() if uncalibrated
    const char* zone_id;               // BIM Zone identifier (e.g. "zone_1")
    const char* sensor_id;             // Hardware sensor identifier (e.g. "esp32_cam_01")
    
    // Optional extensions
    uint16_t inference_time_ms;        // Time spent in TFLite Micro inference (0 if omitted)
    float fps;                         // Current capture + inference FPS (0.0 if unset)
    const struct TrackingBoundingBox* bboxes; // Optional array of bounding boxes (nullptr if none)
    size_t bbox_count;                 // Count of bounding boxes in bboxes array (0 if none)
};

/**
 * @brief Initialize a PersonTrackingData structure with safe defaults.
 */
void initTrackingData(struct PersonTrackingData* data);

/**
 * @brief Validate tracking data fields against physical and logical ranges.
 * @return true if valid, false if invalid.
 */
bool validateTrackingData(const struct PersonTrackingData* data);

/**
 * @brief Serialize PersonTrackingData into a compact JSON string.
 * Zero dynamic memory allocation on hot path.
 * Format: {"sensor_id":"...","zone_id":"...","timestamp_ms":123,"person_detected":true,"confidence":0.94,"person_count":2}
 * 
 * @param data Source tracking data.
 * @param buffer Output character buffer.
 * @param max_len Maximum length of buffer (including null terminator).
 * @return size_t Number of bytes written (excluding null terminator), or 0 on error.
 */
size_t serializeTrackingPayloadPtr(const struct PersonTrackingData* data, char* buffer, size_t max_len);

/**
 * @brief Serialize PersonTrackingData for USB Serial fallback (includes optional _topic).
 * Zero dynamic memory allocation on hot path.
 */
size_t serializeTrackingPayloadForSerialPtr(const struct PersonTrackingData* data, const char* topic, char* buffer, size_t max_len);

/**
 * @brief Serialize PersonTrackingData including optional extended fields (bboxes, inference timing).
 * Zero dynamic memory allocation on hot path.
 */
size_t serializeExtendedTrackingPayloadPtr(const struct PersonTrackingData* data, char* buffer, size_t max_len);

/**
 * @brief Deserialize a JSON payload into a PersonTrackingData structure.
 * Useful for host tests, gateway bridges, and loopback verification.
 */
bool deserializeTrackingPayload(
    const char* json_str,
    size_t len,
    struct PersonTrackingData* out_data,
    char* zone_buf,
    size_t zone_buf_len,
    char* sensor_buf,
    size_t sensor_buf_len
);

#ifdef __cplusplus
}

// C++ Reference-based overloads for seamless ergonomic usage
inline size_t serializeTrackingPayload(const PersonTrackingData& data, char* buffer, size_t max_len) {
    return serializeTrackingPayloadPtr(&data, buffer, max_len);
}

inline size_t serializeTrackingPayload(const PersonTrackingData* data, char* buffer, size_t max_len) {
    return serializeTrackingPayloadPtr(data, buffer, max_len);
}

inline size_t serializeTrackingPayloadForSerial(const PersonTrackingData& data, const char* topic, char* buffer, size_t max_len) {
    return serializeTrackingPayloadForSerialPtr(&data, topic, buffer, max_len);
}

inline size_t serializeTrackingPayloadForSerial(const PersonTrackingData* data, const char* topic, char* buffer, size_t max_len) {
    return serializeTrackingPayloadForSerialPtr(data, topic, buffer, max_len);
}

inline size_t serializeExtendedTrackingPayload(const PersonTrackingData& data, char* buffer, size_t max_len) {
    return serializeExtendedTrackingPayloadPtr(&data, buffer, max_len);
}

inline size_t serializeExtendedTrackingPayload(const PersonTrackingData* data, char* buffer, size_t max_len) {
    return serializeExtendedTrackingPayloadPtr(data, buffer, max_len);
}

#endif
