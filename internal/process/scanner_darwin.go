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
	"unsafe"
)

type darwinScanner struct {
	uid uint32
}

// NewScanner returns the macOS process scanner used by launch verification.
func NewScanner() Scanner {
	return darwinScanner{uid: uint32(os.Getuid())}
}

func (s darwinScanner) FindMain(executablePath, userDataDir string) (Info, bool, error) {
	pids, err := listCurrentUserPIDs(s.uid)
	if err != nil {
		return Info{}, false, err
	}

	for _, pid := range pids {
		executable, err := processPath(pid)
		if err != nil {
			continue
		}
		if filepath.Clean(executable) != filepath.Clean(executablePath) {
			continue
		}

		args, err := processArgs(pid)
		if err != nil {
			continue
		}
		info := Info{PID: pid, ExecutablePath: executable, Args: args}
		if MatchesMainProcess(info, executablePath, userDataDir) {
			return info, true, nil
		}
	}

	return Info{}, false, nil
}

func listCurrentUserPIDs(uid uint32) ([]int, error) {
	pidCount := int(C.cpr_list_all_pids(nil, 0))
	if pidCount <= 0 {
		return nil, nil
	}

	pidSize := int(unsafe.Sizeof(C.pid_t(0)))
	for {
		pids := make([]C.pid_t, pidCount)
		result := int(C.cpr_list_all_pids(&pids[0], C.int(len(pids)*pidSize)))
		count, retry, err := pidListResult(result, len(pids))
		if err != nil {
			return nil, err
		}
		if retry {
			pidCount = count
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
	if returnedCount > bufferPIDCapacity {
		return returnedCount, true, nil
	}
	return returnedCount, false, nil
}

func processPath(pid int) (string, error) {
	buffer := make([]byte, 4096)
	result := int(C.cpr_pid_path(C.pid_t(pid), (*C.char)(unsafe.Pointer(&buffer[0])), C.uint(len(buffer))))
	if result <= 0 || result > len(buffer) {
		return "", fmt.Errorf("resolve executable path for pid %d", pid)
	}
	return strings.TrimRight(string(buffer[:result]), "\x00"), nil
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
