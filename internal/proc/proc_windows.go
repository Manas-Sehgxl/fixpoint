//go:build windows

package proc

import (
	"os/exec"
	"strconv"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func setGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// killGroup stops the child and its whole process tree. Windows has no
// process-group signal, so taskkill walks the tree.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	if err := kill.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}

func shell(line string) (string, []string) { return "cmd", []string{"/C", line} }
