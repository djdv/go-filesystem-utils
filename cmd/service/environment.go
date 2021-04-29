package service

import (
	"context"
	"fmt"
	"reflect"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/manager"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
)

type (
	daemonEnvironment struct {
		context.Context
		instanceIndex manager.Index
	}
)

func MakeEnvironment(ctx context.Context, _ *cmds.Request) (cmds.Environment, error) {
	env := &daemonEnvironment{
		Context:       ctx,
		instanceIndex: manager.NewIndex(),
	}

	return env, nil
}

func (de *daemonEnvironment) Index() manager.Index { return de.instanceIndex }

func getHostServiceConfig(request *cmds.Request) (*service.Config, error) {
	username, _, err := fscmds.GetStringArgument(request, UsernameParameter)
	if err != nil {
		return nil, err
	}
	platformOptions, err := getPlatformOptions(request, servicePlatformOptions)
	if err != nil {
		return nil, err
	}

	return &service.Config{
		Name:        serviceName,
		DisplayName: serviceDisplayName,
		Description: description,
		UserName:    username,
		Option:      platformOptions,
		Arguments:   []string{Name},
	}, nil
}

type (
	// platformOptions are used to declare a set of platform specific options
	// and the keys to retrieve their settings.
	platformOptions struct {
		stringOptions []fscmds.CmdsParameterSet
		intOptions    []fscmds.CmdsParameterSet
		boolOptions   []fscmds.CmdsParameterSet
	}
)

// generatePlatformOptions transforms a set of typed platformOptions
// into a generic set of `cmds.Option`s.
func generatePlatformOptions(pOpts platformOptions) []cmds.Option {
	platformOptions := make([]cmds.Option, 0,
		len(pOpts.stringOptions)+
			len(pOpts.intOptions)+
			len(pOpts.boolOptions))
	for _, parameter := range pOpts.stringOptions {
		platformOptions = append(platformOptions,
			cmds.StringOption(parameter.Name, parameter.Description))
	}
	for _, parameter := range pOpts.intOptions {
		platformOptions = append(platformOptions,
			cmds.IntOption(parameter.Name, parameter.Description))
	}
	for _, parameter := range pOpts.boolOptions {
		platformOptions = append(platformOptions,
			cmds.BoolOption(parameter.Name, parameter.Description))
	}
	return platformOptions
}

func getPlatformOptions(request *cmds.Request, pOpts platformOptions) (service.KeyValue, error) {
	platformOptions := make(service.KeyValue,
		len(pOpts.stringOptions)+
			len(pOpts.intOptions)+
			len(pOpts.boolOptions))

	for _, set := range []struct {
		parameters []fscmds.CmdsParameterSet
		getMethod  interface{}
	}{
		{
			pOpts.stringOptions,
			fscmds.GetStringArgument,
		},
		// TODO: int opts
		{
			pOpts.boolOptions,
			fscmds.GetBoolArgument,
		},
	} {
		for _, parameter := range set.parameters {
			goArguments := []reflect.Value{
				reflect.ValueOf(request),
				reflect.ValueOf(parameter),
			}
			argument, provided, err := getTypeMagic(set.getMethod, goArguments)
			if err != nil {
				return nil, err
			}
			if provided {
				platformOptions[parameter.Name] = argument
			}
		}
	}

	return platformOptions, nil
}

// gettypeMagic wraps the getArgumentX functions we've defined,
// in a type generic way.
// `getString`, `getBool`, etc. are valid input methods.
func getTypeMagic(method interface{},
	goArguments []reflect.Value) (got reflect.Value, provided bool, err error) {
	returnVector := reflect.ValueOf(method).Call(goArguments)
	got = returnVector[0]
	provided = returnVector[1].Bool()
	maybeErr := returnVector[2].Interface()
	if maybeErr != nil {
		var isErr bool
		if err, isErr = maybeErr.(error); !isErr {
			err = fmt.Errorf("return value is wrong type:\n\twanted:%T\n\tgot:%t",
				err, maybeErr)
		}
	}
	return
}
