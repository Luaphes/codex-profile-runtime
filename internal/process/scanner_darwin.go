//go:build darwin && cgo

package process

/*
#cgo LDFLAGS: -lproc
#include <libproc.h>
#include <sys/sysctl.h>
#include <sys/types.h>

static int cpr_list_all_pids(pid_t *buffer, int buffer_size) {
	return proc_listallpids(buffer, buffer_size);
}

static int cpr_pid_uid(pid_t pid) {
	struct proc_bsdinfo info;
	int result = proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info));
	if (result != (int)sizeof(info)) {
		return -1;
	}
	return (int)info.pbi_uid;
}

static int cpr_pid_path(pid_t pid, char *buffer, unsigned int buffer_size) {
	return proc_pidpath(pid, buffer, buffer_size);
}

static int cpr_pid_metadata(pid_t pid, uint32_t *ppid, uint64_t *start_sec, uint64_t *start_usec) {
	struct proc_bsdinfo info;
	int result = proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info));
	if (result != (int)sizeof(info)) {
		return -1;
	}
	*ppid = info.pbi_ppid;
	*start_sec = info.pbi_start_tvsec;
	*start_usec = info.pbi_start_tvusec;
	return 0;
}

static int cpr_pid_args_size(pid_t pid) {
	int mib[3] = {CTL_KERN, KERN_PROCARGS2, pid};
	size_t size = 0;
	if (sysctl(mib, 3, NULL, &size, NULL, 0) != 0) {
		return -1;
	}
	return (int)size;
}

static int cpr_pid_args(pid_t pid, void *buffer, unsigned int buffer_size) {
	int mib[3] = {CTL_KERN, KERN_PROCARGS2, pid};
	size_t size = buffer_size;
	if (sysctl(mib, 3, buffer, &size, NULL, 0) != 0) {
		return -1;
	}
	return (int)size;
}
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"
)

type darwinScanner struct {
	uid uint32
}

// NewScanner returns the macOS process scanner used by launch verification.
func NewScanner() Scanner {
	return darwinScanner{uid: uint32(os.Getuid())}
}

// NewSnapshotter returns the read-only Darwin process snapshotter.
func NewSnapshotter() Snapshotter {
	return darwinScanner{uid: uint32(os.Getuid())}
}

// NewInspector returns the read-only process capability used by safe stop.
func NewInspector() Inspector {
	return darwinScanner{uid: uint32(os.Getuid())}
}

func (s darwinScanner) FindMain(executablePath, userDataDir string) (Info, bool, error) {
	processes, err := s.Snapshot(executablePath)
	if err != nil {
		return Info{}, false, err
	}

	for _, info := range processes {
		if MatchesMainProcess(info, executablePath, userDataDir) {
			return info, true, nil
		}
	}

	return Info{}, false, nil
}

func (s darwinScanner) Snapshot(executablePath string) ([]Info, error) {
	pids, err := listCurrentUserPIDs(s.uid)
	if err != nil {
		return nil, err
	}

	expectedExecutable := filepath.Clean(executablePath)
	processes := make([]Info, 0)
	for _, pid := range pids {
		executable, err := processPath(pid)
		if err != nil || filepath.Clean(executable) != expectedExecutable {
			continue
		}

		info, err := inspectProcess(pid)
		if err != nil {
			continue
		}
		processes = append(processes, info)
	}
	return processes, nil
}

func (s darwinScanner) SnapshotAll() ([]Info, error) {
	pids, err := listCurrentUserPIDs(s.uid)
	if err != nil {
		return nil, err
	}

	processes := make([]Info, 0, len(pids))
	for _, pid := range pids {
		info, err := inspectProcess(pid)
		if err != nil {
			continue
		}
		processes = append(processes, info)
	}
	return processes, nil
}

func (s darwinScanner) Revalidate(expected Info) (Info, bool, error) {
	if expected.PID <= 0 {
		return Info{}, false, fmt.Errorf("invalid process PID %d", expected.PID)
	}
	if uid := int(C.cpr_pid_uid(C.pid_t(expected.PID))); uid < 0 || uint32(uid) != s.uid {
		return Info{}, false, nil
	}

	current, err := inspectProcess(expected.PID)
	if err != nil {
		return Info{}, false, nil
	}
	if !SameIdentity(expected, current) {
		return current, false, nil
	}
	return current, true, nil
}

func inspectProcess(pid int) (Info, error) {
	executable, err := processPath(pid)
	if err != nil {
		return Info{}, err
	}
	ppid, startTime, err := processMetadata(pid)
	if err != nil {
		return Info{}, err
	}
	args, err := processArgs(pid)
	if err != nil {
		return Info{}, err
	}
	return Info{
		PID:            pid,
		PPID:           ppid,
		StartTime:      startTime,
		ExecutablePath: executable,
		Args:           args,
	}, nil
}

func listCurrentUserPIDs(uid uint32) ([]int, error) {
	pidCount := int(C.cpr_list_all_pids(nil, 0))
	if pidCount <= 0 {
		return nil, nil
	}

	pidSize := int(unsafe.Sizeof(C.pid_t(0)))
	var err error
	pidCount, err = nextPIDCapacity(pidCount, pidCount)
	if err != nil {
		return nil, err
	}
	for {
		pids := make([]C.pid_t, pidCount)
		result := int(C.cpr_list_all_pids(&pids[0], C.int(len(pids)*pidSize)))
		count, retry, err := pidListResult(result, len(pids))
		if err != nil {
			return nil, err
		}
		if retry {
			pidCount, err = nextPIDCapacity(len(pids), count)
			if err != nil {
				return nil, err
			}
			continue
		}

		currentUser := make([]int, 0, count)
		for _, rawPID := range pids[:count] {
			pid := int(rawPID)
			if pid <= 0 || int(C.cpr_pid_uid(C.pid_t(pid))) != int(uid) {
				continue
			}
			currentUser = append(currentUser, pid)
		}
		return currentUser, nil
	}
}

// pidListResult treats proc_listallpids' return value as a PID count, while
// the C buffer argument remains sized in bytes.
func pidListResult(returnedCount, bufferPIDCapacity int) (int, bool, error) {
	if returnedCount < 0 {
		return 0, false, fmt.Errorf("enumerate processes")
	}
	if returnedCount >= bufferPIDCapacity {
		return returnedCount, true, nil
	}
	return returnedCount, false, nil
}

func nextPIDCapacity(current, reported int) (int, error) {
	if current <= 0 {
		return 0, fmt.Errorf("invalid process buffer capacity %d", current)
	}
	if current > int(^uint(0)>>1)/2 {
		return 0, fmt.Errorf("process buffer capacity overflow")
	}

	next := current * 2
	if reported > next {
		next = reported
	}
	return next, nil
}

func processPath(pid int) (string, error) {
	buffer := make([]byte, 4096)
	result := int(C.cpr_pid_path(C.pid_t(pid), (*C.char)(unsafe.Pointer(&buffer[0])), C.uint(len(buffer))))
	if result <= 0 || result > len(buffer) {
		return "", fmt.Errorf("resolve executable path for pid %d", pid)
	}
	return strings.TrimRight(string(buffer[:result]), "\x00"), nil
}

func processMetadata(pid int) (int, time.Time, error) {
	var ppid C.uint32_t
	var startSeconds C.uint64_t
	var startMicroseconds C.uint64_t
	if result := C.cpr_pid_metadata(C.pid_t(pid), &ppid, &startSeconds, &startMicroseconds); result != 0 {
		return 0, time.Time{}, fmt.Errorf("resolve metadata for pid %d", pid)
	}
	return int(ppid), processStartTime(uint64(startSeconds), uint64(startMicroseconds)), nil
}

func processStartTime(seconds, microseconds uint64) time.Time {
	return time.Unix(int64(seconds), int64(microseconds)*1000).Local()
}

func processArgs(pid int) ([]string, error) {
	bufferSize := int(C.cpr_pid_args_size(C.pid_t(pid)))
	if bufferSize <= 0 {
		return nil, fmt.Errorf("read argv size for process %d", pid)
	}
	buffer := make([]byte, bufferSize)
	result := int(C.cpr_pid_args(C.pid_t(pid), unsafe.Pointer(&buffer[0]), C.uint(len(buffer))))
	if result <= 0 || result > len(buffer) {
		return nil, fmt.Errorf("read argv for process %d", pid)
	}
	data := buffer[:result]
	if len(data) < 4 {
		return nil, fmt.Errorf("process %d returned truncated argv data", pid)
	}

	argc := int(int32(binary.LittleEndian.Uint32(data[:4])))
	if argc <= 0 || argc > 4096 {
		return nil, fmt.Errorf("process %d returned invalid argc", pid)
	}

	offset := 4
	for offset < len(data) && data[offset] == 0 {
		offset++
	}
	if offset >= len(data) {
		return nil, fmt.Errorf("process %d returned no executable path", pid)
	}
	for offset < len(data) && data[offset] != 0 {
		offset++
	}
	for offset < len(data) && data[offset] == 0 {
		offset++
	}

	args := make([]string, 0, argc)
	for i := 0; i < argc; i++ {
		if offset > len(data) {
			return nil, fmt.Errorf("process %d returned truncated argv", pid)
		}
		start := offset
		for offset < len(data) && data[offset] != 0 {
			offset++
		}
		args = append(args, string(data[start:offset]))
		for offset < len(data) && data[offset] == 0 {
			offset++
		}
	}
	return args, nil
}
