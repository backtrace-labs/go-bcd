// +build !linux arm

package bcd

import (
	"errors"
)

func gettid() (int, error) {
	return 0, errors.New("Gettid() is unsupported on this system")
}

// Call this function to allow other (non-parent) processes to trace this one.
//
// This is a Linux-specific utility function and is stubbed out on other
// operating systems.
func EnableTracing() error {
	return nil
}
