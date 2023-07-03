package generic

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
