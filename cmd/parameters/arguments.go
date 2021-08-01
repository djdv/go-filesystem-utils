package parameters

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/multiformats/go-multiaddr"
)

type (
	// FIXME: [Ame] docs outdated
	// "actual argument"
	Argument struct {
		Parameter
		// ValueReference abstractly refers to an argument's data.
		// (Typically a pointer to a Settings field
		// that will be assigned to)
		ValueReference interface{}
		// I.e `process --serverFlag="localhost"`
		//                ↑Parameter ↑Value -> *ValueRef <- &Settings.TypedValue
		// (abstract key-value like YAML, JSON, et al.)
		// `serverOption: localhost`
		//  ↑Parameter    ↑Value -> *ValueRef <- &Settings.TypedValue
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

func referenceFromField(field reflect.StructField, fieldValue reflect.Value) (interface{}, error) {
	if !fieldValue.CanSet() {
		var (
			err = fmt.Errorf(
				"field (of type `%s`) is not settable",
				field.Type.Name(),
			)
		)
		// TODO: When Go 1.17 is released
		// if !field.IsExported() {
		if field.PkgPath != "" {
			err = fmt.Errorf("%w (the field is not exported)",
				err)
		}
		return nil, err
	}
	return fieldValue.Addr().Interface(), nil
}

func AccumulateArgs(ctx context.Context,
	unsetArgs ArgumentList, inputErrs <-chan error) (unset []Argument, err error) {
	var errs []error
out:
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
				break out
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
			break out
		}
	}
	if len(errs) > 0 {
		err = errs[0]
		for _, e := range errs[1:] {
			err = fmt.Errorf("%w\n%s", err, e)
		}
	}
	return
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

func expandArgument(argument *Argument) (*reflect.Value, reflect.Type, error) {
	var (
		leftValue = reflect.ValueOf(argument.ValueReference)
		leftType  = leftValue.Type()
		assignErr = func() error {
			return fmt.Errorf(
				"cannot assign to argument `%s` reference `%T`",
				argument.CommandLine(),
				argument.ValueReference,
			)
		}
	)
	if leftKind := leftType.Kind(); leftKind != reflect.Ptr {
		return nil, nil, fmt.Errorf("%w - expecting pointer, got: %v",
			assignErr(), leftKind,
		)
	}
	if isNil := leftValue.IsNil(); isNil {
		return nil, nil, fmt.Errorf("%w.IsNil() returned %t",
			assignErr(), isNil)
	}

	leftValue = leftValue.Elem()
	leftType = leftValue.Type()

	if canSet := leftValue.CanSet(); !canSet {
		return nil, nil, fmt.Errorf("%w.CanSet() returned %t",
			assignErr(), canSet,
		)
	}

	return &leftValue, leftType, nil
}

func assignToArgument(argument *Argument, value interface{}) error {
	var (
		leftValue, leftType, err = expandArgument(argument)
		rightValue               = reflect.ValueOf(value)
		rightType                = rightValue.Type()

		durationKind = reflect.TypeOf((*time.Duration)(nil)).Elem().Kind()
	)
	if err != nil {
		return err
	}

	// TODO: [Ame] Sloppy. Handling for special cases could be better.
	switch leftType.Kind() {
	case reflect.Interface:
		intfVal, intfType, err := expandInterfaceArgument(leftType, value)
		if err != nil {
			return err
		}
		rightValue, rightType = *intfVal, intfType
	case reflect.Slice:
		maddrType := reflect.TypeOf((*multiaddr.Multiaddr)(nil)).Elem()
		if leftType.Elem().Implements(maddrType) {
			var (
				maddrStrings, ok = value.([]string)
				maddrs           = make([]multiaddr.Multiaddr, len(maddrStrings))
			)
			if !ok {
				return fmt.Errorf("left value (%#v) is a slice, right value (%#v) is not",
					argument, value,
				)
			}
			for i, maddr := range maddrStrings {
				if maddrs[i], err = multiaddr.NewMultiaddr(maddr); err != nil {
					return err
				}
			}
			value = maddrs
			rightValue = reflect.ValueOf(value)
			rightType = rightValue.Type()
		}
	case durationKind:
		if _, isDuration := leftValue.Interface().(time.Duration); !isDuration {
			break // Argument is an int64, not specifically a time.Duration.
		}
		durationString, isString := value.(string)
		if !isString {
			return fmt.Errorf("expected %T, got %T", durationString, value)
		}
		duration, err := time.ParseDuration(durationString)
		if err != nil {
			return err
		}
		value = duration
		rightValue = reflect.ValueOf(value)
		rightType = rightValue.Type()
	}

	if convertableTo := rightType.ConvertibleTo(leftType); convertableTo {
		rightValue = rightValue.Convert(leftType)
		rightType = leftType
	}

	assignableTo := rightType.AssignableTo(leftType)
	if !assignableTo {
		return fmt.Errorf("`%v`.AssignableTo(`%v`) returned %t",
			rightType, leftType, assignableTo,
		)
	}
	leftValue.Set(rightValue)
	return nil
}

func expandInterfaceArgument(leftType reflect.Type, value interface{}) (rightValue *reflect.Value,
	rightType reflect.Type, err error) {
	maddrType := reflect.TypeOf((*multiaddr.Multiaddr)(nil)).Elem()
	if leftType.Implements(maddrType) {
		var (
			maddrString, isString = value.(string)
			maddr                 multiaddr.Multiaddr
		)
		if !isString {
			err = fmt.Errorf("Expected multiaddr string, got: %T", value)
			return
		}
		if maddr, err = multiaddr.NewMultiaddr(maddrString); err != nil {
			return
		}
		maddrValue := reflect.ValueOf(maddr)
		rightValue = &maddrValue
		rightType = rightValue.Type()
	}
	return
}
