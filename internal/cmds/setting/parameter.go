package settings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	cmdsruntime "github.com/djdv/go-filesystem-utils/internal/cmds/setting/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
)

type CmdsParameter struct {
	Namespace,
	OptionName,
	HelpText,
	EnvPrefix string
	OptionAliases []string
}

// TODO: remove?
var errUnexpectedSourceID = errors.New("unexpected source ID")

func (parameter CmdsParameter) Description() string { return parameter.HelpText }
func (param CmdsParameter) Name(source parameter.Provider) string {
	switch source {
	case parameter.CommandLine:
		return cliName(param.OptionName)
	case parameter.Environment:
		return envName(param.EnvPrefix, param.Namespace, param.OptionName)
	default:
		err := fmt.Errorf("%w: %v", errUnexpectedSourceID, source)
		panic(err)
	}
}

func (param CmdsParameter) Aliases(source parameter.Provider) []string {
	aliases := make([]string, 0, len(param.OptionAliases))
	switch source {
	case parameter.CommandLine:
		for _, name := range param.OptionAliases {
			aliases = append(aliases, cliName(name))
		}
		return aliases
	case parameter.Environment:
		var (
			prefix    = param.EnvPrefix
			namespace = param.Namespace
		)
		for _, name := range param.OptionAliases {
			aliases = append(aliases, envName(prefix, namespace, name))
		}
		return aliases
	default:
		err := fmt.Errorf("%w: %v", errUnexpectedSourceID, source)
		panic(err)
	}
}

// TODO re-do docs (things changed)
//
// NewParameter constructs a parameter using either the provided options,
// or a set of defaults (derived from the calling function's name, pkg, and binary name).
func MustMakeParameters[setPtr cmdsruntime.SettingsType[settings], settings any](ctx context.Context,
	partialParams []CmdsParameter,
) parameter.Parameters {
	fields, err := cmdsruntime.ReflectFields[setPtr](ctx)
	if err != nil {
		panic(err)
	}

	var (
		fieldCount = cap(fields)
		paramCount = len(partialParams)
	)
	if fieldCount < paramCount {
		err := fmt.Errorf("%T: more parameters than fields (%d > %d)",
			(*settings)(nil), paramCount, fieldCount)
		panic(err)
	}

	callersLocation, _, _, ok := runtime.Caller(1)
	if !ok {
		panic("runtime could not get program counter address of function caller")
	}
	return populatePartials(ctx, callersLocation, fields, partialParams)
}

func populatePartials(ctx context.Context, callersLocation uintptr,
	fields cmdsruntime.SettingsFields, partialParams []CmdsParameter,
) parameter.Parameters {
	params := make(chan parameter.Parameter, len(partialParams))
	go func() {
		defer close(params)
		namespace, envPrefix := programMetadata(callersLocation)
		for _, param := range partialParams {
			select {
			case field, ok := <-fields:
				if !ok {
					return
				}
				for _, pair := range []struct {
					currentValue *string
					defaultValue string
				}{
					{&param.OptionName, field.Name},
					{&param.Namespace, namespace},
					{&param.EnvPrefix, envPrefix},
				} {
					if *pair.currentValue == "" {
						*pair.currentValue = pair.defaultValue
					}
				}
				select {
				case params <- param:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return params
}

func programMetadata(funcLocation uintptr) (namespace, envPrefix string) {
	namespace = pkgName(funcLocation)
	envPrefix = execName()
	return
}

func execName() string {
	progName := filepath.Base(os.Args[0])
	return strings.TrimSuffix(progName, filepath.Ext(progName))
}
