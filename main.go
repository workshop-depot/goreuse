package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	setAppInfo(app)
	addCommands(app)

	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
	}
}

func addCommands(app *cli.App) {
	app.Action = cmdApp

	cmdsrv := cli.Command{
		Name:   "server",
		Action: cmdServer,
		Usage:  "keeps annotated code in sync",
	}

	cmdfile := cli.Command{
		Name:   "file",
		Action: cmdFile,
		Usage:  "keeps files in sync (adopts package name accordingly)",
	}
	cmdfile.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "origin",
			Usage:       "path to origin file",
			Destination: &θfileOrigin,
		},
		cli.StringFlag{
			Name:        "dst",
			Usage:       "path to destination file",
			Destination: &θfileDestination,
		},
		cli.StringSliceFlag{
			Name: "rename,r",
			Usage: `-r newName+=oldName -r newName2=oldName2
	the += means preserve definition
	names must be valid Go identifiers"`,
			Value: &θsymbolRename,
		}}

	app.Commands = append(app.Commands, cmdsrv, cmdfile)
}

func setAppInfo(app *cli.App) {
	app.Version = "0.0.1"
	app.Author = "dc0d"
	app.Copyright = "dc0d"
	app.Name = "goreuse"
}
