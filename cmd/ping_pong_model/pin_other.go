//go:build !linux

package main

func pinCurrentThread(mask string) error {
	_ = mask
	return nil
}
