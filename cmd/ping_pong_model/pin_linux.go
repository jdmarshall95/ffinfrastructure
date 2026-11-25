//go:build linux

package main

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
)

func pinCurrentThread(mask string) error {
	if mask == "" || mask == "*" {
		return nil
	}
	runtime.LockOSThread()
	var set unix.CPUSet
	for i, c := range mask {
		if c == '1' {
			set.Set(i)
		}
	}
	if err := unix.SchedSetaffinity(0, &set); err != nil {
		return fmt.Errorf("sched_setaffinity: %w", err)
	}
	return nil
}
