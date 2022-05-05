package runtime

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type (
	// TODO: Name. SettingsType?
	SettingsConstraint[Settings any] interface {
		*Settings           // Type parameter must be pointer to struct
		parameters.Settings // which implements the Settings interface.
	}
)

var (
	// TODO: [review] should these be exported? Probably, but double check.
	ErrUnassignable   = errors.New("cannot assign")
	ErrUnexpectedType = errors.New("unexpected type")
)

func checkType[settings any]() (reflect.Type, error) {
	typ := reflect.TypeOf((*settings)(nil)).Elem()
	if kind := typ.Kind(); kind != reflect.Struct {
		err := fmt.Errorf("%w:"+
			" got: `%s`"+
			" want: `struct`",
			ErrUnexpectedType,
			kind,
		)
		return nil, err
	}
	return typ, nil
}
