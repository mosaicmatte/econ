package main

import (
	"log"
	"os"
	"strings"
)

// isDebugEnabled returns true if LOG_LEVEL is DEBUG or DEBUG is set in environment.
func isDebugEnabled() bool {
	lvl := strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL")))
	if lvl == "DEBUG" {
		return true
	}
	dbg := strings.ToLower(strings.TrimSpace(os.Getenv("DEBUG")))
	return dbg == "1" || dbg == "true" || dbg == "yes"
}

// debugLog writes a formatted log line with a [debug] prefix if debug logging is enabled.
func debugLog(format string, v ...any) {
	if isDebugEnabled() {
		log.Printf("[debug] "+format, v...)
	}
}
