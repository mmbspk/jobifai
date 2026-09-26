//go:build !linux

package browser

// InitVirtualDisplay is a no-op on non-Linux (macOS/Windows use a real display).
func InitVirtualDisplay() error { return nil }
