package cgofuse

import (
	"github.com/u-root/uio/ulog"
)

type (
	wrapperSettings struct {
		log ulog.Logger
		fuseHostSettings
	}
	fuseHostSettings struct {
		readdirPlus,
		deleteAccess,
		caseInsensitive bool
	}
	WrapperOption func(*wrapperSettings) error
)

func parseOptions[ST any, OT ~func(*ST) error](settings *ST, options ...OT) error {
	for _, setFunc := range options {
		if err := setFunc(settings); err != nil {
			return err
		}
	}
	return nil
}

func WithLog(log ulog.Logger) WrapperOption {
	return func(set *wrapperSettings) error { set.log = log; return nil }
}

func SetReaddirPlus(b bool) WrapperOption {
	return func(set *wrapperSettings) error { set.readdirPlus = b; return nil }
}

func SetDeleteAccess(b bool) WrapperOption {
	return func(set *wrapperSettings) error { set.deleteAccess = b; return nil }
}

func SetCaseInsensitive(b bool) WrapperOption {
	return func(set *wrapperSettings) error { set.caseInsensitive = b; return nil }
}
