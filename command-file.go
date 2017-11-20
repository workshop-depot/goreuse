package main

import "github.com/urfave/cli"

func cmdFile(c *cli.Context) error {
	if err := syncFile(fileSource, fileDestination, typeRename...); err != nil {
		logerr.Printf("%+v", err)
	}
	return nil
}
