//go:build !windows

/* Copyright 2022 Adam Pritchard. Licensed under Apache License 2.0. */

package main

import (
	"io"
	"syscall"
)

// From https://stackoverflow.com/a/58664631/729729
func connCheck(sc syscall.Conn) error {
	var sysErr error = nil
	rc, err := sc.SyscallConn()
	if err != nil {
		return err
	}
	err = rc.Read(func(fd uintptr) bool {
		var buf []byte = []byte{0}
		n, _, err := syscall.Recvfrom(int(fd), buf, syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
		switch {
		case n == 0 && err == nil:
			sysErr = io.EOF
		case err == syscall.EAGAIN || err == syscall.EWOULDBLOCK:
			sysErr = nil
		default:
			sysErr = err
		}
		return true
	})
	if err != nil {
		return err
	}

	return sysErr
}
