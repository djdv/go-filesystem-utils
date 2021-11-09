package parameters

import (
	"context"
	"fmt"
	"reflect"
)

const (
	settingsTagKey   = "settings"
	settingsTagValue = "arguments"
)

func tagString() string { return fmt.Sprintf("`%s:\"%s\"`", settingsTagKey, settingsTagValue) }

func typeName(settingsType reflect.Type) string {
	settingName := settingsType.Name()
	if pkg := settingsType.PkgPath(); pkg != "" {
		settingName = fmt.Sprintf("%s.%s", pkg, settingName)
	}
	return settingName
}

type SettingsSource interface {
	setEach(ctx context.Context,
		argsToSet ArgumentList,
		inputErrors <-chan error) (unsetArgs ArgumentList, errs <-chan error)
}

func checkTypeFor(structPtr Settings) (reflect.Type, error) {
	const settingsErrFmt = "expected Settings to be a pointer to struct, got: %T"
	st := reflect.TypeOf(structPtr)
	if st.Kind() != reflect.Ptr {
		return nil, fmt.Errorf(settingsErrFmt, structPtr)
	}
	if st = st.Elem(); st.Kind() != reflect.Struct {
		return nil, fmt.Errorf(settingsErrFmt, structPtr)
	}
	return st, nil
}

func fieldsFrom(ctx context.Context,
	st reflect.Type, fieldOffset int) <-chan reflect.StructField {
	var (
		fieldCount     = st.NumField()
		settingsFields = make(chan reflect.StructField, fieldCount-fieldOffset)
	)
	go func() {
		defer close(settingsFields)
		for i := fieldOffset; i < fieldCount; i++ {
			select {
			case settingsFields <- st.Field(i):
			case <-ctx.Done():
				return
			}
		}
	}()
	return settingsFields
}
