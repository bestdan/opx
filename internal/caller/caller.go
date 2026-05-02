// Package caller identifies the process that invoked opx.
package caller

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Name returns the executable name of the process that spawned the current
// process (i.e. opx's parent).  Returns "unknown" if it cannot be determined.
func Name() string {
	ppid := os.Getppid()

	// Fast path on Linux: read from /proc without spawning a subprocess.
	if runtime.GOOS == "linux" {
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", ppid)); err == nil {
			return strings.TrimSpace(string(b))
		}
	}

	// macOS and fallback: delegate to ps(1).
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(ppid)).Output()
	if err != nil {
		return "unknown"
	}
	// ps output may include the full path; keep only the base name.
	name := strings.TrimSpace(string(out))
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}
