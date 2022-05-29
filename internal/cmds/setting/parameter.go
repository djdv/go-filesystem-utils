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

	return populatePartials2(ctx, fields, partialParams)
}

func populatePartials2(ctx context.Context, fields cmdsruntime.SettingsFields,
	partialParams []CmdsParameter,
) parameter.Parameters {
	var (
		params               = make(chan parameter.Parameter, len(partialParams))
		namespace, envPrefix = programMetadata()
	)
	go func() {
		defer close(params)
		fillIn := func(sPtr *string, s string) {
			if *sPtr == "" {
				*sPtr = s
			}
		}
		for _, param := range partialParams {
			select {
			case field, ok := <-fields:
				if !ok {
					return
				}
				fieldName := field.Name
				fillIn(&param.OptionName, fieldName)
				fillIn(&param.HelpText, fmt.Sprintf(
					"Dynamic parameter for %s", // TODO: This might be a bad idea.
					fieldName,                  // maybe we should leave it blank?
				))
				fillIn(&param.Namespace, namespace)
				param.EnvPrefix = envPrefix
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

func programMetadata() (namespace, envPrefix string) {
	funcLocation, _, _, ok := runtime.Caller(2)
	if !ok {
		panic("runtime could not get program counter address for function")
	}
	namespace, _ = funcNames(funcLocation)
	// FIXME:
	// Rather than filtering ourselves, we need a way for the caller to tell us
	// to just not use a namespace.
	if namespace == "settings" {
		namespace = ""
	}

	envPrefix = execName()
	return
}

func execName() string {
	progName := filepath.Base(os.Args[0])
	return strings.TrimSuffix(progName, filepath.Ext(progName))
}
