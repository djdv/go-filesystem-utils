package p9

import (
	"context"
	"fmt"
	"reflect"
	"unsafe"
)

// XXX: We're using reflection to work around
// a constraint in the 1.18 Go spec. Specifically regarding
// generic access to common struct fields.
// "We may remove this restriction in a future release"
// It's possible to implement this without reflection
// today [1.20], but it requires a lot of duplicate
// type switch cases. (1 for each type for each option).
// We'll take the runtime hits until the aforementioned
// compiler constraint is renounced.
// As an aside, variadic type parameters could also help
// to alleviate things related to pass-through, but these
// are also not approved nor implemented yet in the Go compiler.

type reflectFunc = func([]reflect.Value) (results []reflect.Value)

func parseOptions[ST any, OT ~func(*ST) error](settings *ST, options ...OT) error {
	for _, setFunc := range options {
		if err := setFunc(settings); err != nil {
			return err
		}
	}
	return nil
}

func typeOf[T any]() reflect.Type {
	return reflect.TypeOf([0]T{}).Elem()
}

// XXX: defeat CanSet/CanAddr guard for unexported fields.
func settableValue(value reflect.Value) reflect.Value {
	srcAddr := unsafe.Pointer(value.UnsafeAddr())
	return reflect.NewAt(value.Type(), srcAddr).Elem()
}

// makeFieldSetter is a reflect (unsafe) analog of
// `struct.'name` = value.
func makeFieldSetter[OT Options, FT any](name string, value FT) OT {
	var (
		optionType = typeOf[OT]()
		fieldFunc = func(vp *FT) error {
			*vp = value
			return nil
		}
		rFn = makeFieldReflectFunc(name, optionType, fieldFunc)
	)
	return makeOptionFunc[OT](optionType, rFn)
}

// makeFieldFunc is a reflect (unsafe) analog of
// calling fieldFunc with `&struct.'name`.
func makeFieldFunc[OT Options, FT any](name string, fieldFunc func(*FT) error) OT {
	var (
		optionType = typeOf[OT]()
		rFn        = makeFieldReflectFunc(name, optionType, fieldFunc)
	)
	return makeOptionFunc[OT](optionType, rFn)
}

// NOTE: the returned function panics intentionally
// if there is lack of cohesion for a reference's
// name or type in the source code.
// E.g. field's name|type is changed at its declaration,
// but not in the parameters to this call.
// It would be wise to write tests that assure
// these calls stay in sync and do not panic at runtime.
func makeFieldReflectFunc[FT any](name string, optionType reflect.Type,
	fieldFunc func(*FT) error,
) reflectFunc {
	return func(args []reflect.Value) (results []reflect.Value) {
		var (
			structPtr            = args[0]
			direct, passThroughs = gatherFields[FT](structPtr, name)
			canSet               = direct.IsValid()
			canPassThrough       = passThroughs != nil
			neither              = !canSet && !canPassThrough
		)
		if neither {
			panic(fmt.Sprintf(
				"could not set field"+
					"\ntype `%T` must contain field \"%s\" or a slice"+
					" of options to types that will eventually"+
					" contain this field",
				structPtr.Interface(), name,
			))
		}
		var err error
		if canSet {
			err = reflectCall(direct, name, fieldFunc)
		}
		for _, passThrough := range passThroughs {
			appendToPassThrough(passThrough, name, fieldFunc)
		}
		var ret reflect.Value
		if err != nil {
			ret = reflect.ValueOf(err)
		} else {
			ret = reflect.Zero(optionType.Out(0))
		}
		return []reflect.Value{ret}
	}
}

func makeOptionFunc[OT Options](optTyp reflect.Type, fn reflectFunc) OT {
	return reflect.MakeFunc(optTyp, fn).Interface().(OT)
}

func gatherFields[FT any](structPtr reflect.Value, name string) (direct reflect.Value, passThroughs []reflect.Value) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		fieldType  = typeOf[FT]()
		structType = structPtr.Elem().Type()
		fields     = getFields(ctx, structType)
		funcSlices []reflect.StructField
	)
	for field := range fields {
		if field.Name == name &&
			field.Type == fieldType {
			structValue := structPtr.Elem()
			direct = structValue.FieldByIndex(field.Index)
			continue
		}
		if isSliceOfFunc(field.Type) {
			funcSlices = append(funcSlices, field)
		}
	}
	for _, field := range funcSlices {
		funcType := field.Type.Elem()
		if !isPassThroughField(name, funcType, fieldType) {
			continue
		}
		var (
			structValue = structPtr.Elem()
			passThrough = structValue.FieldByIndex(field.Index)
		)
		passThroughs = append(passThroughs, passThrough)
	}
	return direct, passThroughs
}

func getFields(ctx context.Context, structType reflect.Type) <-chan reflect.StructField {
	var (
		numFields = structType.NumField()
		fields    = make(chan reflect.StructField, numFields)
	)
	go func() {
		root := reflect.StructField{Type: structType}
		splayStuct(ctx, root, fields)
		close(fields)
	}()
	return fields
}

func splayStuct(ctx context.Context,
	field reflect.StructField, fields chan<- reflect.StructField,
) {
	var (
		typ    = field.Type
		prefix = field.Index
	)
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	for i := 0; i < typ.NumField(); i++ {
		subField := typ.Field(i)
		subField.Index = prefixIndex(prefix, subField.Index)
		select {
		case fields <- subField:
		case <-ctx.Done():
			return
		}
		if isEmbeddedStruct(subField) {
			splayStuct(ctx, subField, fields)
		}
	}
}

func prefixIndex(prefix, index []int) []int {
	var (
		pLen     = len(prefix)
		newIndex = make([]int, pLen+len(index))
	)
	copy(newIndex, prefix)
	copy(newIndex[pLen:], index)
	return newIndex
}

func isEmbeddedStruct(field reflect.StructField) bool {
	return field.Anonymous && // Embedded type?
		field.Type.Kind() == reflect.Struct || // Embedded struct?
		(field.Type.Kind() == reflect.Pointer && // Embedded *struct?
			field.Type.Elem().Kind() == reflect.Struct)
}

func isSliceOfFunc(fieldType reflect.Type) bool {
	if fieldType.Kind() == reflect.Slice {
		return fieldType.Elem().Kind() == reflect.Func
	}
	return false
}

func getOptionParameter(funcType reflect.Type) (parameter reflect.Type, ok bool) {
	if funcType.NumIn() != 1 || funcType.NumOut() != 1 {
		return parameter, false
	}
	if funcType.Out(0).Kind() != reflect.Interface {
		return parameter, false
	}
	inType := funcType.In(0)
	if inType.Kind() != reflect.Pointer {
		return parameter, false
	}
	return inType.Elem(), true
}

func isPassThroughField(name string, funcType, fieldType reflect.Type) bool {
	structType, ok := getOptionParameter(funcType)
	if !ok {
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for field := range getFields(ctx, structType) {
		currentType := field.Type
		if isSliceOfFunc(currentType) {
			funcType := field.Type.Elem()
			if ok := isPassThroughField(name, funcType, fieldType); ok {
				return true
			}
			continue
		}
		// FIXME: we need to recur
		// what conditions?
		// this holds true?
		// but also expand slices of options?
		if field.Name == name &&
			currentType == fieldType {
			return true
		}
	}
	return false
}

func reflectCall[FT any](field reflect.Value, name string, fieldFunc func(*FT) error) error {
	var (
		settable     = settableValue(field)
		addr         = settable.Addr().Interface()
		fieldPtr, ok = addr.(*FT)
	)
	if !ok {
		panic(fmt.Sprintf(
			"could not set field"+
				"\nfunction parameter for \"%s\" is type `%T`"+
				" but must be `%T`",
			name, fieldPtr, addr,
		))
	}
	return fieldFunc(fieldPtr)
}

func appendToPassThrough[FT any](
	fieldValue reflect.Value, name string,
	fn func(*FT) error,
) {
	var (
		optionType = fieldValue.Type().Elem()
		setFn      = makeFieldReflectFunc(name, optionType, fn)
		fnValue    = reflect.MakeFunc(optionType, setFn)
		setter     = settableValue(fieldValue)
		newSlice   = reflect.Append(setter, fnValue)
	)
	setter.Set(newSlice)
}
