//go:build !windows

package osutil

import "bytes"

func ensureDirPrivileged(dir string) error {
	cmd, err := SudoCommand("mkdir", "-p", dir)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func writeFilePrivilegedFallback(path string, data []byte) error {
	cmd, err := SudoCommand("tee", path)
	if err != nil {
		return err
	}
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = nil
	return cmd.Run()
}

func removeFilePrivilegedFallback(path string) error {
	cmd, err := SudoCommand("rm", "-f", path)
	if err != nil {
		return err
	}
	return cmd.Run()
}
