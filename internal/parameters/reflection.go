package parameters

import (
	"context"
	"encoding/csv"
	"fmt"
	"reflect"
	"strings"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
)

const (
	settingsTagKey   = "parameters"
	settingsTagValue = "settings"
)

type (
	structFields <-chan reflect.StructField

	paramField struct {
		Parameter
		reflect.StructField
	}
	paramFields = <-chan paramField
	paramBridge = <-chan paramFields

	// structTagPair exists mainly for String formatting,
	// but also for reducing/clarifying function arity/parameters.
	structTagPair struct{ key, value string }

	// providedFunc helps simplify implementations of `Source.SetEach`.
	// (See also: the `setEach` function.)
	providedFunc func(arg Argument) (provided bool, err error)
)

func newStructTagPair(key, value string) structTagPair {
	return structTagPair{
		key:   key,
		value: value,
	}
}

func (tag structTagPair) String() string {
	return fmt.Sprintf("`%s:\"%s\"`", tag.key, tag.value)
}

func hasTagValue(field reflect.StructField, tag structTagPair) (bool, error) {
	if tagStr, ok := field.Tag.Lookup(tag.key); ok {
		fieldTags, err := csv.NewReader(strings.NewReader(tagStr)).Read()
		if err != nil {
			return false, fmt.Errorf("could not parse tag value `%s` as CSV: %w",
				tagStr, err)
		}
		for _, fieldTag := range fieldTags {
			if fieldTag == tag.value {
				return true, nil
			}
		}
	}
	return false, nil
}

func checkType(structPtr Settings) (reflect.Type, error) {
	var (
		pointerType = reflect.TypeOf(structPtr)
		makeErr     = func() error {
			return fmt.Errorf("%w:"+
				" got: %T"+
				" want: pointer to struct",
				errUnexpectedType,
				structPtr,
			)
		}
	)
	if pointerType.Kind() != reflect.Ptr {
		return nil, makeErr()
	}
	if structType := pointerType.Elem(); structType.Kind() == reflect.Struct {
		return structType, nil
	}
	return nil, makeErr()
}

// setEach can be used by `Source.SetEach` implementations
// so that only the `providedFunc` needs to be implemented per `Source`.
func setEach(ctx context.Context,
	providedFn providedFunc, argsToSet Arguments,
) (Arguments, errCh) {
	var (
		unsetArgs = make(chan Argument, cap(argsToSet))
		errs      = make(chan error)
	)
	go func() {
		defer close(unsetArgs)
		defer close(errs)
		for arg := range argsToSet {
			provided, err := providedFn(arg)
			if err != nil {
				select {
				case errs <- err:
				case <-ctx.Done():
				}
				return
			}
			if provided {
				continue
			}
			select {
			case unsetArgs <- arg:
			case <-ctx.Done():
				return
			}
		}
	}()
	return unsetArgs, errs
}

func generateFields(ctx context.Context, setTyp reflect.Type) structFields {
	var (
		fieldCount = setTyp.NumField()
		fields     = make(chan reflect.StructField, fieldCount)
	)
	go func() {
		defer close(fields)
		for i := 0; i < fieldCount; i++ {
			if ctx.Err() != nil {
				return
			}
			fields <- setTyp.Field(i)
		}
	}()
	return fields
}

func expandFields(ctx context.Context, fields structFields) structFields {
	out := make(chan reflect.StructField, cap(fields))
	go func() {
		subCtx, cancel := context.WithCancel(ctx)
		defer close(out)
		defer cancel()
		relayOrExpand := func(field reflect.StructField) error {
			if !field.Anonymous ||
				field.Type.Kind() != reflect.Struct {
				select {
				case out <- field:
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			}
			var (
				embeddedFields = generateFields(subCtx, field.Type)
				prefixedFields = prefixIndex(subCtx, field.Index, embeddedFields)
				recursedFields = expandFields(subCtx, prefixedFields)
			)
			for field := range recursedFields {
				select {
				case out <- field:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}
		ForEachOrError(ctx, fields, nil, relayOrExpand)
	}()
	return out
}

func prefixIndex(ctx context.Context, prefix []int, fields structFields) structFields {
	prefixed := make(chan reflect.StructField, cap(fields))
	go func() {
		defer close(prefixed)
		descend := func(field reflect.StructField) (reflect.StructField, error) {
			field.Index = append(prefix, field.Index...)
			return field, nil
		}
		ProcessResults(ctx, fields, prefixed, nil, descend)
	}()
	return prefixed
}

func fieldsAfterTag(ctx context.Context, tag structTagPair,
	fields structFields,
) (structFields, errCh) {
	var (
		out  = make(chan reflect.StructField, cap(fields))
		errs = make(chan error)
	)
	go func() {
		defer close(out)
		defer close(errs)
		var (
			sawTag          bool
			filterBeforeTag = func(field reflect.StructField) (reflect.StructField, error) {
				if !sawTag {
					var err error
					if sawTag, err = hasTagValue(field, tag); err != nil {
						return reflect.StructField{}, err
					}
					if !sawTag {
						return reflect.StructField{}, ErrSkip
					}
				}
				return field, nil
			}
		)
		ProcessResults(ctx, fields, out, errs, filterBeforeTag)
		if !sawTag {
			err := fmt.Errorf("%w: %s", errNoTag, tag)
			select {
			case errs <- err:
			case <-ctx.Done():
			}
		}
	}()
	return out, errs
}

func bindParameterFields(ctx context.Context,
	typ reflect.Type, parameters Parameters,
) (paramFields, errCh) {
	var (
		subCtx, cancel = context.WithCancel(ctx)
		baseFields     = generateFields(subCtx, typ)
		allFields      = expandFields(subCtx, baseFields)

		tag                   = newStructTagPair(settingsTagKey, settingsTagValue)
		taggedFields, tagErrs = fieldsAfterTag(subCtx, tag, allFields)

		paramCount    = len(parameters)
		reducedFields = CtxTakeAndCancel(subCtx, cancel, taggedFields, paramCount)

		paramFields = make(chan paramField, paramCount)
		bindErrs    = make(chan error)

		errs = CtxMerge(ctx, tagErrs, bindErrs)
	)
	go func() {
		defer close(paramFields)
		defer close(bindErrs)
		var (
			paramIndex int
			bindParams = func(field reflect.StructField) (paramField, error) {
				var (
					parameter = parameters[paramIndex]
					binding   = paramField{
						Parameter:   parameter,
						StructField: field,
					}
				)
				paramIndex++
				return binding, nil
			}
		)
		ProcessResults(ctx, reducedFields, paramFields, nil, bindParams)
		if ctx.Err() != nil {
			return // Don't validate if we're canceled.
		}
		if err := checkParameterCount(paramIndex, paramCount, typ, parameters); err != nil {
			select {
			case bindErrs <- err:
			case <-ctx.Done():
			}
		}
	}()

	return paramFields, errs
}

func referenceFromField(field reflect.StructField, fieldValue reflect.Value) (interface{}, error) {
	if !fieldValue.CanSet() {
		err := fmt.Errorf("%w"+
			" field %s in type `%s` is not settable",
			errUnassignable,
			field.Name, field.Type.Name(),
		)
		if !field.IsExported() {
			err = fmt.Errorf("%w (the field is not exported)",
				err)
		}
		return nil, err
	}
	return fieldValue.Addr().Interface(), nil
}

func checkParameterCount(count, expected int, typ reflect.Type,
	parameters Parameters,
) (err error) {
	if count != expected {
		remainder := parameters[count:]
		err = fmt.Errorf("%w:"+
			"\n\tgot: %d for %s"+
			"\n\twant: %d to fit remaining parameters [%s]",
			errTooFewFields,
			count, typ.Name(),
			expected, remainder,
		)
	}
	return
}
