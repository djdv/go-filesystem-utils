package keyfs

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	ipfs "github.com/djdv/go-filesystem-utils/filesystem/ipfscore"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

const rootName = "."

type keyInterface struct {
	creationTime time.Time
	ctx          context.Context
	core         coreiface.CoreAPI
	ipns         fs.FS
}

// TODO: WithIPFS(fs) option - re-use existing fs.FS instance from daemon
func NewInterface(ctx context.Context, core coreiface.CoreAPI) fs.FS {
	return &keyInterface{
		creationTime: time.Now(),
		ctx:          ctx,
		core:         core,
		ipns:         ipfs.NewInterface(ctx, core, filesystem.IPNS),
	}
}

func (*keyInterface) ID() filesystem.ID { return filesystem.KeyFS }

// TODO: probably inefficient. Review.
// We should at least cache the key list for N seconds
func (ki *keyInterface) translateName(name string) (string, error) {
	keys, err := ki.core.Key().List(ki.ctx)
	if err != nil {
		return "", err
	}
	var (
		components = strings.Split(name, "/")
		keyName    = components[0]
	)
	for _, key := range keys {
		if key.Name() == keyName {
			keyName = pathWithoutNamespace(key)
			break
		}
	}
	components = append([]string{keyName}, components[1:]...)
	keyName = strings.Join(components, "/")
	return keyName, nil
}

func pathWithoutNamespace(key coreiface.Key) string {
	var (
		keyPath = key.Path()
		prefix  = fmt.Sprintf("/%s/", keyPath.Namespace())
	)
	return strings.TrimPrefix(keyPath.String(), prefix)
}

func (ki *keyInterface) Open(name string) (fs.File, error) {
	const op errors.Op = "keyfs.Open"
	if name == rootName {
		return ki.OpenDir(name)
	}
	translated, err := ki.translateName(name)
	if err != nil {
		return nil, err
	}
	return ki.ipns.Open(translated)
}

func (ki *keyInterface) OpenDir(name string) (fs.ReadDirFile, error) {
	const op errors.Op = "keyfs.OpenDir"
	if name == rootName {
		ctx, cancel := context.WithCancel(ki.ctx)
		return &keyDirectory{
			ctx: ctx, cancel: cancel,
			stat:   (*rootStat)(&ki.creationTime),
			keyAPI: ki.core.Key(),
			ipns:   ki.ipns,
		}, nil
	}

	ipns, ok := ki.ipns.(filesystem.OpenDirFS)
	if !ok {
		// TODO: better message
		return nil, errors.New(op,
			"OpenDir not supported by the provided IPFS fs.FS",
		)
	}

	translated, err := ki.translateName(name)
	if err != nil {
		return nil, err
	}

	return ipns.OpenDir(translated)
}

// TODO: close everything
func (*keyInterface) Close() error { return nil }

func (*keyInterface) Rename(_, _ string) error {
	const op errors.Op = "keyfs.Rename"
	// TODO: use abstract, consistent, error values
	// (^ this means reimplementing pkg `iferrors` with new Go conventions)
	//return errReadOnly
	return errors.New(op, errors.InvalidOperation)
}
