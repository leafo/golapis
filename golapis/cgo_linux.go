//go:build linux

package golapis

/*
#cgo LDFLAGS: -L../luajit/src -l:libluajit.a -lm -ldl -rdynamic
*/
import "C"
