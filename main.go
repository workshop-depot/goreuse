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
			Name:        "src",
			Usage:       "path to source file",
			Destination: &fileSource,
		},
		cli.StringFlag{
			Name:        "dst",
			Usage:       "path to destination file",
			Destination: &fileDestination,
		},
		cli.StringSliceFlag{
			Name:  "type-rename,t",
			Usage: "-t NEW_TYPE=OLD_TYPE -t NEW_TYPE_2=OLD_TYPE_2",
			Value: &typeRename,
		},
	}

	app.Commands = append(app.Commands, cmdsrv, cmdfile)
}

func setAppInfo(app *cli.App) {
	app.Version = "0.0.1"
	app.Author = "dc0d"
	app.Copyright = "dc0d"
	app.Name = "goreuse"
}
