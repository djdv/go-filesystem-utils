package p9

import (
	"context"
	"fmt"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type (
	ChannelFile struct {
		templatefs.NoopFile
		metadata
		linkSync *linkSync
		emitter  *chanEmitter[[]byte]
		openFlags
	}
	channelSettings struct {
		fileOptions
		buffer int
	}
	ChannelOption func(*channelSettings) error
)

func NewChannelFile(ctx context.Context,
	options ...ChannelOption,
) (p9.QID, *ChannelFile, <-chan []byte, error) {
	var settings channelSettings
	if err := parseOptions(&settings, options...); err != nil {
		return p9.QID{}, nil, nil, err
	}
	metadata, err := makeMetadata(p9.ModeRegular, settings.metaOptions...)
	if err != nil {
		return p9.QID{}, nil, nil, err
	}
	linkSync, err := newLinkSync(settings.linkOptions...)
	if err != nil {
		return p9.QID{}, nil, nil, err
	}
	var (
		emitter   = makeChannelEmitter[[]byte](ctx, settings.buffer)
		bytesChan = emitter.ch
		chanFile  = &ChannelFile{
			metadata: metadata,
			linkSync: linkSync,
			emitter:  emitter,
		}
	)
	metadata.incrementPath()
	return *chanFile.QID, chanFile, bytesChan, nil
}

func WithBuffer[OT ChannelOptions](size int) OT {
	return makeFieldSetter[OT]("buffer", size)
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
	return *cf.QID, 0, nil
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
