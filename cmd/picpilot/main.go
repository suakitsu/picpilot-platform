package main

import (
	"os"

	"github.com/suakitsu/picpilot-platform-go/core/app"
	"github.com/suakitsu/picpilot-platform-go/core/config"
)

func main() { os.Exit(app.Run(config.Platform, os.Args[1:], os.Stdout, os.Stderr)) }
