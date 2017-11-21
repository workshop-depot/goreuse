package main

import (
	"github.com/urfave/cli"
)

func cmdFile(c *cli.Context) error {
	if err := dsync().syncFile(θfileOrigin, θfileDestination, θsymbolRename); err != nil {
		logerr.Printf("%+v", err)
	}
	return nil
}
