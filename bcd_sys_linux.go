// +build !arm

package bcd

import (
	sys "golang.org/x/sys/unix"
)

func gettid() (int, error) {
	return sys.Gettid(), nil
}

// Call this function to allow other (non-parent) processes to trace this one.
// Alternatively, set kernel.yama.ptrace_scope = 0 in
// /etc/sysctl.d/10-ptrace.conf.
//
// This is a Linux-specific utility function.
func EnableTracing() error {
	return sys.Prctl(sys.PR_SET_PTRACER, sys.PR_SET_PTRACER_ANY, 0, 0, 0)
}
