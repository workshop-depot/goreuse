package main

import (
	stdlog "log"
	"os"

	"github.com/urfave/cli"
)

// flags
var (
	θfileOrigin      string
	θfileDestination string
	θsymbolRename    = cli.StringSlice([]string{})
)

var (
	logerr *stdlog.Logger
	loginf *stdlog.Logger
	logwrn *stdlog.Logger
)

func init() {
	var flag int
	flag = stdlog.Lshortfile

	logerr = stdlog.New(os.Stderr, "level=err ", flag)
	loginf = stdlog.New(os.Stdout, "level=inf ", flag)
	logwrn = stdlog.New(os.Stdout, "level=wrn ", flag)
}
