//go:build !debug

package golapis

const debugEnabled = false

// debugLog is defined but never called in release builds.
// The if debugEnabled { } blocks are eliminated by the compiler,
// so this function is never actually invoked.
func debugLog(format string, args ...interface{}) {}
