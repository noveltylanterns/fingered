package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func main() {
	name := filepath.Base(os.Args[0])

	switch name {
	case "echo.cgi":
		echo()
	case "control.cgi":
		control()
	case "nul.cgi":
		nul()
	case "big.cgi":
		big()
	case "slow.cgi":
		slow()
	case "fail.cgi":
		fail()
	case "index.cgi":
		indexSentinel()
	case "inspect.cgi":
		inspect()
	default:
		fmt.Printf("unknown probe: %s\n", name)
	}
}

func readRequest() string {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1024))
	if err != nil {
		return "stdin-read-error"
	}
	return strings.TrimSuffix(string(data), "\n")
}

func echo() {
	fmt.Printf("cgi-echo:%s\n", readRequest())
}

func control() {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("cgi-control:")
	buf.WriteByte(0x01)
	buf.WriteByte('\t')
	buf.WriteByte(0x7f)
	buf.WriteByte('\n')
	_, _ = os.Stdout.Write(buf.Bytes())
}

func nul() {
	_, _ = os.Stdout.Write([]byte{'o', 'k', 0x00, '\n'})
}

func big() {
	chunk := strings.Repeat("A", 8192)
	for i := 0; i < 64; i++ {
		fmt.Println(chunk)
	}
}

func slow() {
	time.Sleep(2500 * time.Millisecond)
	fmt.Println("cgi-slow-complete")
}

func fail() {
	fmt.Fprintln(os.Stderr, "cgi probe stderr marker")
	os.Exit(7)
}

func indexSentinel() {
	fmt.Println("cgi-index-should-not-win")
}

type capHeader struct {
	Version uint32
	Pid     int32
}

type capData struct {
	Effective   uint32
	Permitted   uint32
	Inheritable uint32
}

const (
	prGetNoNewPrivs = 0x27
	capVersion3     = 0x20080522
)

func inspect() {
	uid := syscall.Geteuid()
	gid := syscall.Getegid()
	groups, _ := syscall.Getgroups()
	envCount := len(os.Environ())
	cwd, _ := os.Getwd()
	fds := openFDs(3, 16)

	eff, prm, inh, capErr := capabilities()
	noNewPrivs, nnpErr := getNoNewPrivs()
	openCfgErr := tryOpen("/etc/fingered/fingered.conf")
	bindErr := tryBindLow()
	chrootErr := tryChroot()

	fmt.Printf("uid=%d\n", uid)
	fmt.Printf("gid=%d\n", gid)
	fmt.Printf("groups=%s\n", joinInts(groups))
	fmt.Printf("env_count=%d\n", envCount)
	fmt.Printf("cwd=%s\n", cwd)
	fmt.Printf("fds=%s\n", joinInts(fds))
	if capErr != nil {
		fmt.Printf("cap_err=%s\n", capErr)
	} else {
		fmt.Printf("caps_eff=0x%x\n", eff)
		fmt.Printf("caps_prm=0x%x\n", prm)
		fmt.Printf("caps_inh=0x%x\n", inh)
	}
	if nnpErr != nil {
		fmt.Printf("no_new_privs_err=%s\n", nnpErr)
	} else {
		fmt.Printf("no_new_privs=%d\n", noNewPrivs)
	}
	fmt.Printf("open_config=%s\n", openCfgErr)
	fmt.Printf("bind_low=%s\n", bindErr)
	fmt.Printf("chroot_again=%s\n", chrootErr)
}

func openFDs(start, end int) []int {
	var out []int
	for fd := start; fd <= end; fd++ {
		var st syscall.Stat_t
		if err := syscall.Fstat(fd, &st); err == nil {
			out = append(out, fd)
		}
	}
	return out
}

func capabilities() (uint64, uint64, uint64, error) {
	hdr := capHeader{Version: capVersion3}
	data := [2]capData{}
	_, _, errno := syscall.RawSyscall(syscall.SYS_CAPGET, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data[0])), 0)
	if errno != 0 {
		return 0, 0, 0, errno
	}
	eff := uint64(data[1].Effective)<<32 | uint64(data[0].Effective)
	prm := uint64(data[1].Permitted)<<32 | uint64(data[0].Permitted)
	inh := uint64(data[1].Inheritable)<<32 | uint64(data[0].Inheritable)
	return eff, prm, inh, nil
}

func getNoNewPrivs() (uintptr, error) {
	r1, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, prGetNoNewPrivs, 0, 0, 0, 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return r1, nil
}

func tryOpen(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return err.Error()
	}
	_ = f.Close()
	return "ok"
}

func tryBindLow() string {
	ln, err := net.Listen("tcp", "127.0.0.1:1")
	if err != nil {
		return err.Error()
	}
	_ = ln.Close()
	return "ok"
}

func tryChroot() string {
	if err := syscall.Chroot("."); err != nil {
		return err.Error()
	}
	return "ok"
}

func joinInts(values []int) string {
	if len(values) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ",")
}
