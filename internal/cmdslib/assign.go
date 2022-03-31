package cmdslib

import (
	"fmt"
	"reflect"
)

func AssignToArgument(arg Argument, value interface{}, parsers ...TypeParser) error {
	var (
		leftValue  = reflect.ValueOf(arg.ValueReference).Elem()
		leftType   = leftValue.Type()
		rightValue = reflect.ValueOf(value)
		rightType  = rightValue.Type()
	)

	if canAssignDirectly(leftType, rightType) {
		leftValue.Set(rightValue)
		return nil
	}

	assigned, err := maybeAssignCustom(leftType, leftValue, value, parsers)
	if err != nil {
		return fmt.Errorf("custom handler: %w", err)
	}
	if assigned {
		return nil
	}

	convertedValue, err := maybeConvert(leftType, rightType, rightValue, parsers)
	if err != nil {
		return fmt.Errorf("type conversion: %w", err)
	}
	if convertedValue != nil {
		rightValue = *convertedValue
		rightType = rightValue.Type()
	}

	if !leftType.AssignableTo(rightType) {
		err := fmt.Errorf("%w: `%#v` to %v",
			ErrUnassignable, rightValue.Interface(), leftType,
		)
		return err
	}

	leftValue.Set(rightValue)
	return nil
}

func canAssignDirectly(leftType, rightType reflect.Type) bool {
	return leftType == rightType ||
		(leftType.Kind() == reflect.Interface &&
			rightType.Implements(leftType))
}

func maybeAssignCustom(leftType reflect.Type, leftValue reflect.Value,
	rightValue interface{}, parsers typeParsers) (bool, error,
) {
	parser := parsers.Index(leftType)
	if parser == nil {
		return false, nil
	}
	if err := assignCustomType(parser.ParseFunc, leftValue, rightValue); err != nil {
		return false, fmt.Errorf("failed to assign to `%v`: %w", leftType, err)
	}
	return true, nil
}

func assignCustomType(parse ParseFunc, leftValue reflect.Value, rightValue interface{}) error {
	valueStr, isString := rightValue.(string)
	if !isString {
		return fmt.Errorf("%w for parser argument - got: %T want: %T",
			ErrUnexpectedType, valueStr, rightValue,
		)
	}
	customValue, err := parse(valueStr)
	if err != nil {
		return fmt.Errorf("failed to parse: %w", err)
	}
	leftValue.Set(reflect.ValueOf(customValue))
	return nil
}
