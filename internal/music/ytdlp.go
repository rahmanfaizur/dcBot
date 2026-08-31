package music

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
)

const ytdlpCacheDir = "/tmp/yt-dlp-cache"

func ytdlpCommonArgs() []string {
	return []string{
		"--no-playlist",
		"--no-warnings",
		"--no-progress",
		"--cache-dir", ytdlpCacheDir,
		"--extractor-args", "youtube:player_client=web,mweb,android",
	}
}

func captureYTDLPOutput(cmd *exec.Cmd) (string, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	var outBuf, errBuf bytes.Buffer
	outDone := make(chan error, 1)
	errDone := make(chan error, 1)
	go func() { _, err := io.Copy(&outBuf, stdout); outDone <- err }()
	go func() { _, err := io.Copy(&errBuf, stderr); errDone <- err }()

	if err := <-outDone; err != nil {
		_ = cmd.Wait()
		return "", err
	}
	if err := <-errDone; err != nil {
		_ = cmd.Wait()
		return "", err
	}

	waitErr := cmd.Wait()
	stdoutText := strings.TrimSpace(outBuf.String())
	stderrText := strings.TrimSpace(errBuf.String())

	if waitErr != nil {
		if stderrText != "" {
			return stdoutText, &ytdlpExecError{err: waitErr, stderr: stderrText}
		}
		return stdoutText, waitErr
	}
	return stdoutText, nil
}

type ytdlpExecError struct {
	err    error
	stderr string
}

func (e *ytdlpExecError) Error() string {
	if e.stderr != "" {
		return e.err.Error() + ": " + e.stderr
	}
	return e.err.Error()
}

func (e *ytdlpExecError) Unwrap() error {
	return e.err
}
