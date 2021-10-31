package main

import "fmt"

// Version info. This is set by the Makefile
var (
	version string
	commit  string
	builtBy string
)

var versionInfo = fmt.Sprintf("Version: %s\nCommit: %s\nBuilt by: %s\n", version, commit, builtBy)
