package service

import (
	"fmt"
	"strings"

	"github.com/djdv/go-filesystem-utils/cmd/service/host"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
)

// registerControllerCommands inserts Command wrappers
// for each service control-action provided.
func registerControllerCommands(subcommands map[string]*cmds.Command,
	actions ...string) {
	for _, action := range actions {
		action := action
		subcommands[action] = &cmds.Command{
			NoRemote: true,
			Encoders: cmds.Encoders,
			Helptext: cmds.HelpText{
				Tagline: fmt.Sprintf("%s the service.", strings.Title(action)),
			},
			Run: func(request *cmds.Request,
				_ cmds.ResponseEmitter, _ cmds.Environment) (err error) {
				ctx := request.Context
				settings, err := parseSettings(ctx, request)
				if err != nil {
					return err
				}
				serviceConfig := host.ServiceConfig(&settings.Host)
				serviceClient, err := service.New((service.Interface)(nil), serviceConfig)
				if err != nil {
					return err
				}
				// NOTE: We don't currently emit anything here besides errors.
				// (Something like `print("${Control}: Okay")` could be done if desired.)
				//
				// If there's an error it will be returned and encoded|printed.
				// Otherwise output is nothing with exit_code = success.
				return service.Control(serviceClient, action)
			},
		}
	}
}
