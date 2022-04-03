package config

import (
	"log"

	cmds "github.com/ipfs/go-ipfs-cmds"
)

const Name = "config"

func Command() *cmds.Command {
	return &cmds.Command{
		Helptext: cmds.HelpText{
			Tagline: "TODO",
		},
		NoRemote: true,
		Run:      run,
	}
}

func run(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	log.Println("config DBG")
	return nil
}
