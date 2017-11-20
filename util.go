package main

import (
	stdlog "log"
	"os"

	"github.com/urfave/cli"
)

// flags
var (
	fileSource      string
	fileDestination string
	typeRename      = cli.StringSlice([]string{})
)

var (
	logerr = stdlog.New(os.Stderr, "level=err ", 0)
	loginf = stdlog.New(os.Stdout, "level=inf ", 0)
	logwrn = stdlog.New(os.Stdout, "level=wrn ", 0)
)
