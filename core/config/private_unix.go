//go:build !windows

package config

import "os"

func protectPrivateFile(path string) error { return os.Chmod(path, 0600) }
