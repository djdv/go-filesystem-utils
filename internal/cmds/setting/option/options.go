// Package option provides generic constructors for the cmdslib `Option` interface.
package option

import (
	"context"
	"fmt"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// MakeOptions creates cmdslib `Option`s
// using underlying type and interface data from [*settings].
func MakeOptions[setPtr runtime.SettingsType[settings],
	settings any](options ...ConstructorOption,
) ([]cmds.Option, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fieldAndParams, errs, err := fieldParamsFromSettings[setPtr](ctx)
	if err != nil {
		return nil, err
	}
	var (
		maybeBuiltin       []cmds.Option
		constructorOptions = parseConstructorOptions(options...)
		userConstructors   = constructorOptions.userConstructors
	)
	if constructorOptions.withBuiltin {
		maybeBuiltin = builtinOptions()
	}

	cmdsOptions := make([]cmds.Option, 0, cap(fieldAndParams)+len(maybeBuiltin))
	for pairOrErr := range generic.CtxEither(ctx, fieldAndParams, errs) {
		if err := pairOrErr.Right; err != nil {
			return nil, err
		}
		var (
			fieldAndParam = pairOrErr.Left
			field         = fieldAndParam.Left
			param         = fieldAndParam.Right
			option, err   = optionFromField(field, param, userConstructors)
		)
		if err != nil {
			return nil, err
		}
		cmdsOptions = append(cmdsOptions, option)
	}
	return append(cmdsOptions, maybeBuiltin...), nil
}

func optionFromField(field structField, param fieldParameter,
	constructors []Constructor,
) (cmds.Option, error) {
	typ := field.Type

	if userConstructor := maybeGetConstructor(constructors, typ); userConstructor != nil {
		name, desc, aliases := splayParameter(param)
		return userConstructor.NewOption(name, desc, aliases...), nil
	}

	if builtinConstructor := constructorForKind(typ.Kind()); builtinConstructor != nil {
		constructorArgs := parameterToConstructorArgs(param)
		return builtinConstructor(constructorArgs...), nil
	}

	err := fmt.Errorf("%w:"+
		" can't determine which option constructor to use for `%s`"+
		" (type %v with no custom handler)",
		runtime.ErrUnexpectedType,
		field.Name, typ,
	)
	return nil, err
}

func maybeGetConstructor(constructors []Constructor, typ reflect.Type) Constructor {
	for _, constructor := range constructors {
		if constructor.Type() == typ {
			return constructor
		}
	}
	return nil
}

func splayParameter(param parameter.Parameter) (name, description string, aliases []string) {
	name = param.Name(parameter.CommandLine)
	aliases = param.Aliases(parameter.CommandLine)
	description = fmt.Sprintf("%s (Env: %s)",
		param.Description(),
		param.Name(parameter.Environment),
	)
	return
}

func parameterToConstructorArgs(param parameter.Parameter) []string {
	const nameAndDescription = 2
	var (
		name, description, aliases = splayParameter(param)
		optionCount                = len(aliases) + nameAndDescription
		optionArgs                 = make([]string, 0, optionCount)
	)
	// NOTE: cmds lib option constructor determines the purpose of these values by their order.
	// Name is first, aliases follow, and the last argument is the description.
	optionArgs = append(optionArgs, name)
	optionArgs = append(optionArgs, aliases...)
	optionArgs = append(optionArgs, description)

	return optionArgs
}
