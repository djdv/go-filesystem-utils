package cgofuse

import (
	"github.com/u-root/uio/ulog"
	"github.com/winfsp/cgofuse/fuse"
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

func (settings *fuseHostSettings) apply(fsh *fuse.FileSystemHost) {
	for _, pair := range []struct {
		setter func(bool)
		bool
	}{
		{setter: fsh.SetCapReaddirPlus, bool: settings.readdirPlus},
		{setter: fsh.SetCapCaseInsensitive, bool: settings.caseInsensitive},
		{setter: fsh.SetCapDeleteAccess, bool: settings.deleteAccess},
	} {
		pair.setter(pair.bool)
	}
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
