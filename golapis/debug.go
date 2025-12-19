//go:build debug

package golapis

import (
	"fmt"
	"os"
)

const debugEnabled = true

// debugLog prints debug messages when built with -tags debug
func debugLog(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
}
