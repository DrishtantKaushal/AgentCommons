//go:build !windows

package cmd

import "syscall"

func execve(path string, args []string, env []string) error {
	return syscall.Exec(path, args, env)
}
