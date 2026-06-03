//go:build !linux

package main

import "os/exec"

func setupProcessIO(cmd *exec.Cmd) (*processIO, error) {
	return setupPipeProcessIO(cmd)
}
