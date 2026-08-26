// -----------------------------------------------------------------------------
// model_data.h — Quantized int8 TFLite Micro Person Detection Model Data
//
// Stored in Flash (.rodata) with 16-byte alignment to consume 0 bytes of SRAM.
// -----------------------------------------------------------------------------
#pragma once

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

// Pointer to 16-byte aligned model FlatBuffer in Flash (.rodata)
extern const unsigned char g_person_detect_model_data[];

// Length of model FlatBuffer in bytes
extern const unsigned int g_person_detect_model_data_len;

#ifdef __cplusplus
}
#endif
