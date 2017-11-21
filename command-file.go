package main

import (
	"github.com/urfave/cli"
)

func cmdFile(c *cli.Context) error {
	θfileOrigin = "/home/carbon/C/gopath/src/github.com/dc0d/goreuse/wd-sample/lib/lib.go"
	θfileDestination = "/home/carbon/C/gopath/src/github.com/dc0d/goreuse/wd-sample/spec/spec.go"
	if err := dsync().syncFile(θfileOrigin, θfileDestination, θsymbolRename); err != nil {
		logerr.Printf("%+v", err)
	}
	return nil
}
