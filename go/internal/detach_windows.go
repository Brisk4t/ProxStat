//go:build windows

package internal

import "syscall"

// DETACHED_PROCESS, which package syscall doesn't define
const detachedProcess = 0x00000008

// detached starts the daemon without a console and in its own process group, so closing the
// terminal doesn't end it
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}
