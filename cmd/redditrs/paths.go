package main

import (
	"os"
	"path/filepath"
	"strings"
)

func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	prefix := home + string(filepath.Separator)
	if strings.HasPrefix(path, prefix) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
