package parameters

import "context"

type providedFunc func(argument *Argument) (provided bool, err error)

func setEach(ctx context.Context, providedFn providedFunc,
	argsToSet ArgumentList,
	inputErrors <-chan error) (ArgumentList, <-chan error) {
	var (
		unsetArgs = make(chan *Argument, cap(argsToSet))
		errors    = make(chan error, cap(inputErrors))
	)
	go func() {
		defer close(unsetArgs)
		defer close(errors)
		for argsToSet != nil ||
			inputErrors != nil {
			select {
			case argument, ok := <-argsToSet:
				if !ok {
					argsToSet = nil
					continue
				}
				provided, err := providedFn(argument)
				if err != nil {
					select {
					case errors <- err:
					case <-ctx.Done():
					}
				}
				if provided {
					continue
				}
				select { // Relay parameter to next source.
				case unsetArgs <- argument:
				case <-ctx.Done():
				}
			case err, ok := <-inputErrors:
				if !ok {
					inputErrors = nil
					continue
				}
				// If we encounter an input error,
				// relay it and keep processing.
				// The caller may decide to cancel or not afterwards.
				select {
				case errors <- err:
				case <-ctx.Done():
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return unsetArgs, errors
}
