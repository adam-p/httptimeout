//go:build windows

/* Copyright 2022 Adam Pritchard. Licensed under Apache License 2.0. */

package main

import "syscall"

func connCheck(sc syscall.Conn) error {
	return nil
}
