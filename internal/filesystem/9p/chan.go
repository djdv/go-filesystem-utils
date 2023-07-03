package p9

import (
	"context"
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	perrors "github.com/djdv/p9/errors"
	"github.com/djdv/p9/fsimpl/templatefs"
	"github.com/djdv/p9/p9"
)

type (
	ChannelFile struct {
		templatefs.NoopFile
		*metadata
		*linkSync
		emitter *chanEmitter[[]byte]
		openFlags
	}
	channelSettings struct {
		buffer int
	}
	channelFileSettings struct {
		fileSettings
		channelSettings
	}
	ChannelOption        func(*channelFileSettings) error
	channelSetter[T any] interface {
		*T
		setBuffer(int)
	}
)

func (cs *channelSettings) setBuffer(size int) { cs.buffer = size }

func NewChannelFile(ctx context.Context,
	options ...ChannelOption,
) (p9.QID, *ChannelFile, <-chan []byte, error) {
	var settings channelFileSettings
	settings.metadata.initialize(p9.ModeRegular)
	if err := generic.ApplyOptions(&settings, options...); err != nil {
		return p9.QID{}, nil, nil, err
	}
	var (
		emitter = makeChannelEmitter[[]byte](
			ctx,
			settings.buffer,
		)
		bytesChan = emitter.ch
		file      = &ChannelFile{
			metadata: &settings.metadata,
			linkSync: &settings.linkSync,
			emitter:  emitter,
		}
	)
	settings.metadata.fillDefaults()
	settings.metadata.incrementPath()
	return settings.QID, file, bytesChan, nil
}

func WithBuffer[
	OT generic.OptionFunc[T],
	T any,
	I channelSetter[T],
](size int,
) OT {
	return func(channelFile *T) error {
		any(channelFile).(I).setBuffer(size)
		return nil
	}
}

func (cf *ChannelFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) > 0 {
		return nil, nil, perrors.ENOTDIR
	}
	if cf.opened() {
		return nil, nil, fidOpenedErr
	}
	return nil, &ChannelFile{
		metadata: cf.metadata,
		linkSync: cf.linkSync,
		emitter:  cf.emitter,
	}, nil
}

func (cf *ChannelFile) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
	if cf.opened() {
		return p9.QID{}, 0, perrors.EBADF
	}
	if mode.Mode() != p9.WriteOnly {
		// TODO: [spec] correct evalue?
		return p9.QID{}, 0, perrors.EINVAL
	}
	cf.openFlags = cf.withOpenedFlag(mode)
	return cf.QID, 0, nil
}

func (cf *ChannelFile) Close() error {
	cf.openFlags = 0
	return nil
}

func (cf *ChannelFile) WriteAt(p []byte, _ int64) (int, error) {
	if !cf.canWrite() {
		return -1, perrors.EBADF
	}
	if err := cf.emitter.emit(p); err != nil {
		// TODO: spec error value
		// TODO: Go 1.20 will allow multiple %w
		return -1, fmt.Errorf("%w - %s", perrors.EIO, err)
	}
	return len(p), nil
}

func (cf *ChannelFile) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return cf.metadata.SetAttr(valid, attr)
}

func (cf *ChannelFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return cf.metadata.GetAttr(req)
}

func (cf *ChannelFile) Rename(newDir p9.File, newName string) error {
	return cf.linkSync.rename(cf, newDir, newName)
}

func (cf *ChannelFile) Renamed(newDir p9.File, newName string) {
	cf.linkSync.Renamed(newDir, newName)
}
