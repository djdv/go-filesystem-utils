package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type CmdsParameter struct {
	Namespace,
	OptionName,
	HelpText,
	EnvPrefix string
	OptionAliases []string
}

func (parameter CmdsParameter) Description() string { return parameter.HelpText }
func (parameter CmdsParameter) Name(source parameters.SourceID) string {
	switch source {
	case parameters.CommandLine:
		return cliName(parameter.OptionName)
	case parameters.Environment:
		return envName(parameter.EnvPrefix, parameter.Namespace, parameter.OptionName)
	default:
		err := fmt.Errorf("%w: %v", parameters.ErrUnexpectedSourceID, source)
		panic(err)
	}
}

func (parameter CmdsParameter) Aliases(source parameters.SourceID) []string {
	aliases := make([]string, 0, len(parameter.OptionAliases))
	switch source {
	case parameters.CommandLine:
		for _, name := range parameter.OptionAliases {
			aliases = append(aliases, cliName(name))
		}
		return aliases
	case parameters.Environment:
		var (
			prefix    = parameter.EnvPrefix
			namespace = parameter.Namespace
		)
		for _, name := range parameter.OptionAliases {
			aliases = append(aliases, envName(prefix, namespace, name))
		}
		return aliases
	default:
		err := fmt.Errorf("%w: %v", parameters.ErrUnexpectedSourceID, source)
		panic(err)
	}
}

// TODO re-do docs (things changed)
//
// NewParameter constructs a parameter using either the provided options,
// or a set of defaults (derived from the calling function's name, pkg, and binary name).
func MustMakeParameters[setPtr SettingsConstraint[settings], settings any](ctx context.Context,
	partialParams []CmdsParameter,
) parameters.Parameters {
	typ, err := checkType[settings]()
	if err != nil {
		panic(err)
	}
	paramCount := len(partialParams)
	if fieldCount := typ.NumField(); fieldCount < paramCount {
		err := fmt.Errorf("%s: %w (%d > %d)",
			typ.Name(), errTooFewFields, paramCount, fieldCount)
		panic(err)
	}
	return populatePartials(ctx, typ, partialParams, paramCount)
}

// TODO: name
func populatePartials(ctx context.Context, typ reflect.Type,
	partialParams []CmdsParameter, paramCount int,
) parameters.Parameters {
	var (
		params               = make(chan parameters.Parameter, paramCount)
		namespace, envPrefix = programMetadata()
	)
	go func() {
		defer close(params)
		subCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		var (
			fields = generateFields(subCtx, typ)
			fillIn = func(sPtr *string, s string) {
				if *sPtr == "" {
					*sPtr = s
				}
			}
		)
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

// TODO: rename?
func BindParameterFields(ctx context.Context, fields StructFields, params parameters.Parameters) (ParamFields, <-chan error) {
	var (
		paramFields = make(chan ParamField, cap(params))
		errs        = make(chan error)
	)
	go func() {
		defer close(paramFields)
		defer close(errs)
		bindParams := func(field reflect.StructField) (ParamField, error) {
			select {
			case parameter, ok := <-params:
				if !ok {
					// TODO: annotate error with counts? or something.
					return ParamField{}, errTooManyFields
				}
				binding := ParamField{
					Parameter:   parameter,
					StructField: field,
				}
				return binding, nil

			case <-ctx.Done():
				return ParamField{}, ctx.Err()
			}
		}
		generic.ProcessResults(ctx, fields, paramFields, errs, bindParams)
		if err := checkParameterCount(ctx, params); err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
		}
	}()

	return paramFields, errs
}

// TODO: rename?
func bindParameterFields[setPtr SettingsConstraint[set], set any](ctx context.Context,
) (ParamFields, <-chan error, error) {
	typ, err := checkType[set]()
	if err != nil {
		return nil, nil, err
	}
	var (
		// MAGIC: We know our parameter methods don't use data,
		// so we call them directly with nil.
		// This just avoids allocating an unnecessary value.
		// We could instantiate and call `settings.Parameters` if ever needed.
		params            = setPtr.Parameters(nil, ctx)
		baseFields        = generateFields(ctx, typ)
		allFields         = expandFields(ctx, baseFields)
		paramFields, errs = BindParameterFields(ctx, allFields, params)
	)
	return paramFields, errs, nil
}

func checkParameterCount(ctx context.Context, params parameters.Parameters) error {
	var extraParams []string
out:
	for {
		select {
		case extra, ok := <-params:
			if !ok {
				break out
			}
			name := extra.Name(parameters.CommandLine)
			extraParams = append(extraParams, name)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if extraParams != nil {
		errStr := strings.Join(extraParams, ", ")
		return fmt.Errorf("%w - extra parameters: %s", errTooFewFields, errStr)
	}
	return nil
}
