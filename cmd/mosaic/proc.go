package main

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// readProcPids returns the pids of every running process whose
// command line contains "bin/mosaic" or ends in "/mosaic". We shell
// out to pgrep when available because it is the simplest way to
// match against the full command line across macOS and Linux.
func readProcPids() ([]int, error) {
	cmd := exec.Command("pgrep", "-f", "mosaic")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, nil // no matches or pgrep missing — both benign
	}
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// sendSigterm delivers SIGTERM to pid.
func sendSigterm(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
