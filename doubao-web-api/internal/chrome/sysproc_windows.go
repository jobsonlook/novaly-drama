//go:build windows

package chrome

import "syscall"

func chromeSysProcAttr() *syscall.SysProcAttr {
	return nil
}
