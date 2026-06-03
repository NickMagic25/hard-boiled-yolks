package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

const (
	tiocgptn   = 0x80045430
	tiocsptlck = 0x40045431
)

func setupProcessIO(cmd *exec.Cmd) (*processIO, error) {
	pty, err := setupPTYProcessIO(cmd)
	if err == nil {
		return pty, nil
	}
	return setupPipeProcessIO(cmd)
}

func setupPTYProcessIO(cmd *exec.Cmd) (*processIO, error) {
	master, slave, err := openPTY()
	if err != nil {
		return nil, err
	}

	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}

	return &processIO{
		stdin:   master,
		readers: []io.Reader{master},
		afterStart: func() {
			_ = slave.Close()
		},
		cleanup: func() {
			_ = master.Close()
			_ = slave.Close()
		},
	}, nil
}

func openPTY() (*os.File, *os.File, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}

	unlock := int32(0)
	if err := ioctl(master.Fd(), tiocsptlck, uintptr(unsafe.Pointer(&unlock))); err != nil {
		_ = master.Close()
		return nil, nil, err
	}

	var number uint32
	if err := ioctl(master.Fd(), tiocgptn, uintptr(unsafe.Pointer(&number))); err != nil {
		_ = master.Close()
		return nil, nil, err
	}

	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

func ioctl(fd uintptr, request uintptr, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, arg)
	if errno != 0 {
		return errno
	}
	return nil
}
