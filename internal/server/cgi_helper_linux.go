//go:build linux

package server

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	cgiATEmptyPath  = 0x1000
	cgiOpenPath     = 0x200000
	cgiOpenFlags    = cgiOpenPath | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	cgiPRNoNewPrivs = 0x26
	cgiPRCapAmbient = 0x2f
	cgiPRCapClear   = 0x4
	cgiCapVersion3  = 0x20080522
)

func ExecCGIHelper(root, argv0 string, fd int) error {
	if err := validateCGIHelper(root, argv0, fd); err != nil {
		return err
	}
	if err := syscall.Chdir(root); err != nil {
		return fmt.Errorf("chdir cgi root: %w", err)
	}
	if err := syscall.Chroot("."); err != nil {
		return fmt.Errorf("chroot cgi root: %w", err)
	}
	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("chdir jail root: %w", err)
	}
	if err := dropCGIPrivileges(); err != nil {
		return err
	}
	return execveatFD(fd, argv0)
}

func validateCGIHelper(root, argv0 string, fd int) error {
	if root == "" || !filepath.IsAbs(root) {
		return fmt.Errorf("internal cgi root must be an absolute path")
	}
	if argv0 == "" || !strings.HasPrefix(argv0, "/") || strings.Contains(argv0[1:], "/") {
		return fmt.Errorf("internal cgi argv0 must be a jailed absolute filename")
	}
	if fd < 3 {
		return fmt.Errorf("internal cgi fd must be >= 3")
	}
	return nil
}

func execveatFD(fd int, argv0 string) error {
	pathPtr, err := syscall.BytePtrFromString("")
	if err != nil {
		return err
	}
	argv, err := stringPtrs([]string{argv0})
	if err != nil {
		return err
	}
	envv, err := stringPtrs(nil)
	if err != nil {
		return err
	}
	_, _, errno := syscall.RawSyscall6(
		cgiExecveatSyscall,
		uintptr(fd),
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&argv[0])),
		uintptr(unsafe.Pointer(&envv[0])),
		uintptr(cgiATEmptyPath),
		0,
	)
	runtime.KeepAlive(pathPtr)
	runtime.KeepAlive(argv)
	runtime.KeepAlive(envv)
	if errno != 0 {
		return errno
	}
	return syscall.EINVAL
}

type cgiCapHeader struct {
	Version uint32
	Pid     int32
}

type cgiCapData struct {
	Effective   uint32
	Permitted   uint32
	Inheritable uint32
}

func dropCGIPrivileges() error {
	if _, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, cgiPRNoNewPrivs, 1, 0, 0, 0, 0); errno != 0 && errno != syscall.EINVAL {
		return fmt.Errorf("set no_new_privs: %w", errno)
	}

	if _, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, cgiPRCapAmbient, cgiPRCapClear, 0, 0, 0, 0); errno != 0 && errno != syscall.EINVAL {
		return fmt.Errorf("clear ambient capabilities: %w", errno)
	}

	hdr := cgiCapHeader{Version: cgiCapVersion3}
	data := [2]cgiCapData{}
	_, _, errno := syscall.RawSyscall(syscall.SYS_CAPSET, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data[0])), 0)
	runtime.KeepAlive(hdr)
	runtime.KeepAlive(data)
	if errno != 0 {
		return fmt.Errorf("drop capabilities: %w", errno)
	}
	return nil
}

func stringPtrs(values []string) ([]*byte, error) {
	out := make([]*byte, 0, len(values)+1)
	for _, value := range values {
		ptr, err := syscall.BytePtrFromString(value)
		if err != nil {
			return nil, err
		}
		out = append(out, ptr)
	}
	out = append(out, nil)
	return out, nil
}
