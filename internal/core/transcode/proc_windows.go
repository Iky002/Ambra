//go:build windows

package transcode

import "os/exec"

func suspendProcess(_ *exec.Cmd) error {
	return nil
}

func resumeProcess(_ *exec.Cmd) error {
	return nil
}
