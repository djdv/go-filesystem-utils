package parameters

import (
	"context"
	"encoding/csv"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
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
		// Flatten any nested containers via the index value.
		// Separating the field offset, from the container's offset.
		// E.g.
		// index-value: [1][2][3]
		// iteration 0: [1][2] <- subcontainer [3] <- field offset within container 2.
		// iteration 1: [1] subcontainer [2] <- field offset within container 1.
		// iteration 2: [1] <- field offset within container that was passed to the function.
		for sil := len(settingsIndex); argumentsBound != parameterCount &&
			sil != 0; sil = len(settingsIndex) {
			// Pop the offset value from the index
			// and shift the index leftward.
			offset := settingsIndex[sil-1]
			settingsIndex = settingsIndex[:sil-1]
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
							// If the tagged container is embedded
							// in another container, it implies we
							// already processed it (first).
							// Its parent container will always contain that same field,
							// so skip it when we see it a second time
							// (while processing the parent container).
							continue
						}
						// Expand embedded container's fields (in-place).
						fields = fieldsFrom(containerCtx,
							field.Type, 0)
						settingsIndex = append(settingsIndex, field.Index...)
						continue
					}
					// Index into the struct to get the address for its field value.
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
				panic("nil argument was received")
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

		if shouldRecurse(settingsField) {
			settingsFieldBase, err := argumentFieldIn(settingsField.Type)
			if err != nil {
				// An error here implies the tag was not in the embedded struct
				// continue scanning the remaining fields (at this level).
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
		hasArgumentsTag, err := hasTagValue(settingsField, settingsTagKey, settingsTagValue)
		if err != nil {
			return nil, err
		}
		if hasArgumentsTag {
			return &settingsField, nil
		}
	}
	return nil, fmt.Errorf("could not find tag: %s within \"%s\"",
		tagString(), typeName(settingsType),
	)
}

func shouldRecurse(field reflect.StructField) bool {
	// Recurse on embedded structs.
	return field.Type.Kind() == reflect.Struct && field.Anonymous
}

func hasTagValue(field reflect.StructField, key, value string) (bool, error) {
	if tagString, ok := field.Tag.Lookup(key); ok {
		tags, err := csv.NewReader(strings.NewReader(tagString)).Read()
		if err != nil {
			return false, fmt.Errorf("could not parse tag value `%s` as CSV: %w",
				tagString, err)
		}
		for _, tag := range tags {
			if tag == value {
				return true, nil
			}
		}
	}
	return false, nil
}

func assignToArgument(argument *Argument, value interface{}) error {
	var (
		leftValue = reflect.ValueOf(argument.ValueReference).Elem()
		leftType  = leftValue.Type()
		toString  = func(value interface{}) (string, error) {
			typedString, isString := value.(string)
			if !isString {
				return "", fmt.Errorf("expected %T, got %T", typedString, value)
			}
			return typedString, nil
		}
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
			return fmt.Errorf("failed to parse time for %s - %w",
				argument.CommandLine(), err,
			)
		}
		value = duration
	case *multiaddr.Multiaddr:
		if _, isMaddr := value.(multiaddr.Multiaddr); isMaddr {
			break // Direct assign, no parsing.
		}

		maddrString, isString := value.(string)
		if !isString {
			return fmt.Errorf("expected multiaddr string, got: %T", value)
		}
		maddr, err := multiaddr.NewMultiaddr(maddrString)
		if err != nil {
			return fmt.Errorf("failed to parse maddr for %s - %w",
				argument.CommandLine(), err,
			)
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
	case *filesystem.ID:
		fmt.Println("ID case")
		if _, isId := value.(filesystem.ID); isId {
			break // Direct assign, no parsing.
		}

		idString, err := toString(value)
		if err != nil {
			return err
		}
		id, err := filesystem.StringToID(idString)
		if err != nil {
			return err
		}
		value = id
	case *filesystem.API:
		if _, isApi := value.(filesystem.API); isApi {
			break // Direct assign, no parsing.
		}

		apiString, err := toString(value)
		if err != nil {
			return err
		}
		api, err := filesystem.StringToAPI(apiString)
		if err != nil {
			return err
		}
		value = api
	}

	rightValue := reflect.ValueOf(value)
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
		rightType := rightValue.Type()
		if convertableTo := rightType.ConvertibleTo(leftType); convertableTo {
			rightValue = rightValue.Convert(leftType)
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
