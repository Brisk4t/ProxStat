//go:build unix

package internal

import "syscall"

// detached starts the daemon in its own session, so closing the terminal doesn't hang it up
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
