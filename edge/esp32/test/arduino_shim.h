// Minimal host shims so node_config.h can be compiled and tested off-target.
//
// Only what node_config.h actually touches: Serial (printf), and Preferences (NVS). The
// Preferences shim is an in-memory blob store, which is enough to exercise the save/load
// round trip and the "stored blob is the wrong size" path.
#pragma once

#include <cstdarg>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <cmath>
#include <string>
#include <vector>

// --- Serial ------------------------------------------------------------------
struct SerialShim {
  void printf(const char* fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    vprintf(fmt, ap);
    va_end(ap);
  }
  void println(const char* s) { ::printf("%s\n", s); }
  void print(const char* s) { ::printf("%s", s); }
};
static SerialShim Serial;

// --- Preferences (NVS) -------------------------------------------------------
// One process-wide store, matching the single "econ" namespace the firmware uses.
struct PrefStore {
  bool has = false;
  std::vector<uint8_t> blob;
};
inline PrefStore& prefStore() {
  static PrefStore s;
  return s;
}

class Preferences {
public:
  bool begin(const char*, bool) { return true; }
  void end() {}
  size_t putBytes(const char*, const void* p, size_t n) {
    auto& s = prefStore();
    s.blob.assign((const uint8_t*)p, (const uint8_t*)p + n);
    s.has = true;
    return n;
  }
  size_t getBytesLength(const char*) {
    auto& s = prefStore();
    return s.has ? s.blob.size() : 0;
  }
  size_t getBytes(const char*, void* out, size_t n) {
    auto& s = prefStore();
    if (!s.has || s.blob.size() != n) return 0;
    memcpy(out, s.blob.data(), n);
    return n;
  }
  bool remove(const char*) {
    prefStore().has = false;
    return true;
  }
};
