package parameters

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

type (
	// Argument represents the pairing of a Parameter (the formal argument)
	// with its concrete value (the actual argument).
	//  // I.e `process --serverFlag="localhost"`
	//  //                ↑Parameter  ↑Value -> *ValueRef <- &Settings.TypedValue == "localhost"
	//  //  (abstract key-value like YAML, JSON, et al.)
	//  //  `serverOption: localhost`
	//  //   ↑Parameter    ↑Value -> *ValueRef <- &Settings.TypedValue == "localhost"
	Argument struct {
		Parameter
		// ValueReference is typically a pointer to a field within a `Settings` struct,
		// but any abstract reference value is allowed.
		ValueReference interface{}
	}
	ArgumentList <-chan *Argument
)

func ParseSettings(ctx context.Context, set Settings,
	providers ...SettingsSource) (unsetArgs ArgumentList, errs <-chan error) {
	unsetArgs, errs = ArgumentsFrom(ctx, set)
	for _, provider := range providers {
		unsetArgs, errs = provider.setEach(ctx, unsetArgs, errs)
	}
	return
}

func ArgumentsFrom(ctx context.Context, settings Settings) (ArgumentList, <-chan error) {
	singleErr := func(err error) (ArgumentList, <-chan error) {
		ec := make(chan error, 1)
		ec <- err
		close(ec)
		return nil, ec
	}
	settingsType, settingsTypeErr := checkTypeFor(settings)
	if settingsTypeErr != nil {
		return singleErr(settingsTypeErr)
	}
	argumentField0, settingsDeclareErr := argumentFieldIn(settingsType)
	if settingsDeclareErr != nil {
		return singleErr(settingsDeclareErr)
	}
	settingsValue := reflect.ValueOf(settings).Elem()

	return argumentsFrom(ctx,
		settingsValue, settingsType,
		argumentField0.Index, settings.Parameters())
}

func argumentsFrom(ctx context.Context,
	settingsValue reflect.Value, settingsType reflect.Type,
	settingsIndex []int,
	parameters Parameters) (ArgumentList, <-chan error) {
	var (
		argumentsBound int
		parameterCount = len(parameters)
		argumentList   = make(chan *Argument, parameterCount)
		argumentErrs   = make(chan error, 1)

		tagIndex   = settingsIndex[:len(settingsIndex)-1]
		taggedType = settingsType.FieldByIndex(tagIndex).Type
	)
	go func() {
		defer close(argumentList)
		defer close(argumentErrs)
		// TODO: [Ame] Review - all
		// Separate any nested containers from their field's offset.
		// index-value: [1][2][3]
		// iteration 0: [1][2] <- subcontainer [3] <- offset within container 2
		// iter 1: [1] subcontainer [2] offset within container 1
		// iter 2: [1] offset within container that was passed to the function
		for sil := len(settingsIndex); argumentsBound != parameterCount &&
			sil != 0; sil = len(settingsIndex) {
			offset := settingsIndex[sil-1]
			settingsIndex = settingsIndex[:sil-1]
			// Flatten all nested containers via the index value.
			var (
				container                     = settingsType.FieldByIndex(settingsIndex).Type
				containerCtx, containerCancel = context.WithCancel(ctx)
				fields                        = fieldsFrom(containerCtx,
					container, offset)
			)
			defer containerCancel()
			for fields != nil {
				select {
				case field, ok := <-fields:
					if !ok ||
						argumentsBound == parameterCount {
						fields = nil
						containerCancel()
						continue
					}
					if field.Type.Kind() == reflect.Struct &&
						field.Anonymous {
						if field.Type == taggedType {
							// TODO: [Ame] English.
							// If the tagged container was embedded,
							// we already flattened it up front.
							// And the parent will always contain it,
							// so skip it when we encounter it again
							// (while processing its parent).
							continue
						}
						// Expand in place.
						fields = fieldsFrom(containerCtx,
							field.Type, 0)
						settingsIndex = append(settingsIndex, field.Index...)
						continue
					}
					// Finally Index into the struct value
					// (rather than just the type)
					// and get the address for its actual field.
					var (
						fieldIndex          = append(settingsIndex, field.Index...)
						fieldValue          = settingsValue.FieldByIndex(fieldIndex)
						valueReference, err = referenceFromField(field, fieldValue)
					)
					if err != nil {
						argumentErrs <- fmt.Errorf("%s.%s %w",
							container.Name(),
							field.Name,
							err)
						return
					}
					argumentList <- &Argument{
						Parameter:      parameters[argumentsBound],
						ValueReference: valueReference,
					}
					argumentsBound++
				case <-ctx.Done():
					return
				}
			}
		}

		if argumentsBound < parameterCount {
			remainder := parameters[argumentsBound:]
			argumentErrs <- fmt.Errorf(
				"%s doesn't have enough fields declared after settings tag"+
					" - have %d need %d to fit remaining parameters: [%s]",
				settingsType.Name(),
				argumentsBound, parameterCount,
				remainder,
			)
		}
	}()

	return argumentList, argumentErrs
}

func fromRequest(options cmds.OptMap) providedFunc {
	return func(argument *Argument) (provided bool, err error) {
		var (
			cmdsArg         interface{}
			commandlineName = argument.Parameter.CommandLine()
		)
		if cmdsArg, provided = options[commandlineName]; provided {
			if err = assignToArgument(argument, cmdsArg); err != nil {
				err = fmt.Errorf(
					"parameter `%s`: couldn't assign value: %w",
					commandlineName, err)
			}
		}
		return
	}
}

func referenceFromField(field reflect.StructField, fieldValue reflect.Value) (interface{}, error) {
	if !fieldValue.CanSet() {
		err := fmt.Errorf(
			"field (of type `%s`) is not settable",
			field.Type.Name(),
		)
		if !field.IsExported() {
			err = fmt.Errorf("%w (the field is not exported)",
				err)
		}
		return nil, err
	}
	return fieldValue.Addr().Interface(), nil
}

func AccumulateArgs(ctx context.Context,
	unsetArgs ArgumentList, inputErrs <-chan error) (unset []Argument, _ error) {
	var (
		errs        []error
		flattenErrs = func(errs ...error) error {
			if len(errs) > 0 {
				err := errs[0]
				for _, e := range errs[1:] {
					err = fmt.Errorf("%w; %s", err, e)
				}
				return err
			}
			return nil
		}
	)
	for unsetArgs != nil ||
		inputErrs != nil {
		select {
		case argument, ok := <-unsetArgs:
			if !ok {
				unsetArgs = nil
				continue
			}
			if argument == nil {
				// NOTE: This implies an implementation fault
				// exists in the input generator.
				errs = append(errs,
					errors.New("nil argument was received - aborting"))
				return unset, flattenErrs(errs...)
			}
			unset = append(unset, *argument)
		case e, ok := <-inputErrs:
			if !ok {
				inputErrs = nil
				continue
			}
			errs = append(errs, e)
		case <-ctx.Done():
			errs = append(errs, ctx.Err())
			return unset, flattenErrs(errs...)
		}
	}

	return unset, flattenErrs(errs...)
}

func argumentFieldIn(settingsType reflect.Type) (*reflect.StructField, error) {
	// Find argument field via struct tag.
	fieldCount := settingsType.NumField()
	for i := 0; i < fieldCount; i++ {
		settingsField := settingsType.Field(i)
		// Recurs on embedded structs.
		if settingsField.Type.Kind() == reflect.Struct &&
			settingsField.Anonymous {
			settingsFieldBase, err := argumentFieldIn(settingsField.Type)
			if err != nil {
				// An error here implies the tag was not in the embedded struct
				// continue scanning the remaining fields.
				continue
			}
			// Tag was found in an embedded struct,
			// append its index to its parent index.
			settingsFieldBase.Index = append(
				settingsField.Index,
				settingsFieldBase.Index...,
			)
			return settingsFieldBase, nil
		}
		// Look for the tag in the currently selected field.
		if tagString, ok := settingsField.Tag.Lookup(settingsTagKey); ok {
			tags, err := csv.NewReader(strings.NewReader(tagString)).Read()
			if err != nil {
				err = fmt.Errorf("could not parse tag value `%s` as CSV: %w",
					tagString, err)
				return nil, err
			}
			for _, tag := range tags {
				if tag == settingsTagValue {
					return &settingsField, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("could not find tag `%s:\"%s\"` within `%s`",
		settingsTagKey, settingsTagValue, settingsType.Name(),
	)
}

func assignToArgument(argument *Argument, value interface{}) error {
	var (
		leftValue = reflect.ValueOf(argument.ValueReference).Elem()
		leftType  = leftValue.Type()
	)

	// Special type cases.
	switch argument.ValueReference.(type) {
	case *time.Duration:
		if _, isDuration := value.(time.Duration); isDuration {
			break // Direct assign, no parsing.
		}

		durationString, isString := value.(string)
		if !isString {
			return fmt.Errorf("left value is time.Duration, "+
				"right value (%#v) is expected to be a string",
				value,
			)
		}
		duration, err := time.ParseDuration(durationString)
		if err != nil {
			return err
		}
		value = duration
	case *multiaddr.Multiaddr:
		if _, isMaddr := value.(multiaddr.Multiaddr); isMaddr {
			break // Direct assign, no parsing.
		}

		maddrString, isString := value.(string)
		if !isString {
			return fmt.Errorf("Expected multiaddr string, got: %T", value)
		}
		maddr, err := multiaddr.NewMultiaddr(maddrString)
		if err != nil {
			return err
		}
		value = maddr
	case *[]multiaddr.Multiaddr:
		if _, isMaddrSlice := value.([]multiaddr.Multiaddr); isMaddrSlice {
			break // Direct assign, no parsing.
		}
		var (
			maddrStrings, ok = value.([]string)
			maddrs           = make([]multiaddr.Multiaddr, len(maddrStrings))
		)
		if !ok {
			return fmt.Errorf("left value is a maddr slice, "+
				"right value (%#v) is expected to be slice of strings",
				value,
			)
		}
		for i, maddr := range maddrStrings {
			var maddrErr error
			if maddrs[i], maddrErr = multiaddr.NewMultiaddr(maddr); maddrErr != nil {
				return maddrErr
			}
		}
		value = maddrs
	}

	var (
		rightValue = reflect.ValueOf(value)
		rightType  = rightValue.Type()
	)
	switch kind := leftType.Kind(); kind {
	case cmds.Bool,
		cmds.Int,
		cmds.Uint,
		cmds.Int64,
		cmds.Uint64,
		cmds.Float,
		cmds.String,
		cmds.Strings,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Int16,
		reflect.Int32,
		reflect.Struct,
		reflect.Slice,
		reflect.Interface:
		if convertableTo := rightType.ConvertibleTo(leftType); convertableTo {
			rightValue = rightValue.Convert(leftType)
			rightType = leftType
		}
	case reflect.Ptr:
		return fmt.Errorf("left value (%#v) uses multiple layers of indirection (not allowed)",
			value,
		)
	default:
		return fmt.Errorf("left value (%#v) has unexpected kind (%v)",
			value, kind,
		)
	}

	leftValue.Set(rightValue)

	return nil
}
