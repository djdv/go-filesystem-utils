package generic

import "fmt"

type OptionFunc[T any] interface {
	~func(*T) error
}

func ApplyOptions[
	OT OptionFunc[T],
	T any,
](settings *T, options ...OT,
) error {
	for _, apply := range options {
		if err := apply(settings); err != nil {
			return err
		}
	}
	return nil
}

func ErrIfOptionWasSet[T comparable](name string, current, dflt T) error {
	if current != dflt {
		return OptionAlreadySet(name)
	}
	return nil
}

func OptionAlreadySet(name string) error {
	return fmt.Errorf("%s option provided multiple times", name)
}
