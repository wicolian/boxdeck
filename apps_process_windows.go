//go:build windows

package main

import "os/exec"

func configureAppProcess(cmd *exec.Cmd) {}

func terminateAppProcess(cmd *exec.Cmd) error { return cmd.Process.Kill() }

func killAppProcess(cmd *exec.Cmd) error { return cmd.Process.Kill() }
