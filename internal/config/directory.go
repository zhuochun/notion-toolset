package config

import (
	"fmt"
	"os"
)

func CheckDirectory(dir string) error {
	pathInfo, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("directory does not exists: %v. Create it first", dir)
	}

	if !pathInfo.IsDir() {
		return fmt.Errorf("directory is invalid: %v", dir)
	}
	return nil
}
