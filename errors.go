package main

import "errors"

var (
	ErrConfigDeprecated = errors.New("Config Deprecated")
	ErrConfigRequired   = errors.New("Config Missing")
)
