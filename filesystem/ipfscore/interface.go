package ipfs

import (
	"context"
	"io/fs"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

const rootName = "."

type coreInterface struct {
	creationTime time.Time
	ctx          context.Context
	core         coreiface.CoreAPI
	systemID     filesystem.ID
}

func NewInterface(ctx context.Context, core coreiface.CoreAPI, systemID filesystem.ID) fs.FS {
	return &coreInterface{
		creationTime: time.Now(),
		ctx:          ctx,
		core:         core,
		systemID:     systemID,
	}
}

func (ci *coreInterface) ID() filesystem.ID { return ci.systemID }

func (ci *coreInterface) Open(name string) (fs.File, error) {
	const op errors.Op = "ipfscore.Open"
	if name == rootName {
		return ci.OpenDir(name)
	}

	if !fs.ValidPath(name) {
		return nil, errors.New(op,
			errors.Path(name),
			errors.InvalidItem,
		)
	}

	// TODO: OpenFile + read-only checking on flags
	const timeout = 10 * time.Second // TODO: we should have a single callTimeout const pkg-wide
	var (
		corePath            = goToIPFSCore(ci.systemID, name)
		callCtx, callCancel = context.WithTimeout(ci.ctx, timeout)
	)
	defer callCancel()
	ipldNode, err := resolveNode(callCtx, ci.core, corePath)
	if err != nil {
		return nil, errors.New(op,
			errors.Permission, // TODO: check POSIX spec; this should probably be IO
			errors.Path(name),
			err,
		)
	}

	stat, err := ci.stat(name, ipldNode)
	if err != nil {
		return nil, errors.New(op,
			errors.Path(name),
			errors.IO, // TODO: [review] double check this Kind makes sense for this.
			err,
		)
	}

	var (
		file   fs.File
		fErr   error
		statFn = ci.genStatFunc(name, stat)
	)
	// TODO links, etc.
	switch stat.Mode().Type() {
	case fs.FileMode(0):
		file, fErr = openIPFSFile(ci.ctx, ci.core, ipldNode, statFn)
	case fs.ModeDir:
		file, fErr = openIPFSDir(ci.ctx, ci.core, ipldNode, statFn, ci.creationTime)
	default:
		// TODO: real error value+message
		fErr = errors.New("unsupported type")
	}

	if fErr != nil {
		return nil, errors.New(op,
			errors.Path(name),
			errors.IO, // TODO: [review] double check this Kind makes sense for this.
			fErr,
		)
	}
	return file, nil
}

func (ci *coreInterface) OpenDir(name string) (fs.ReadDirFile, error) {
	const op errors.Op = "ipfscore.OpenDir"
	if name == rootName {
		return (*rootDirectory)(&ci.creationTime), nil
	}

	if !fs.ValidPath(name) {
		return nil, errors.New(op,
			errors.Path(name),
			errors.InvalidItem,
		)
	}

	//TODO: de-dupe between Open
	const timeout = 10 * time.Second // TODO: we should have a single callTimeout const pkg-wide
	var (
		corePath            = goToIPFSCore(ci.systemID, name)
		callCtx, callCancel = context.WithTimeout(ci.ctx, timeout)
	)
	defer callCancel()
	ipldNode, err := resolveNode(callCtx, ci.core, corePath)
	if err != nil {
		return nil, errors.New(op,
			errors.Permission, // TODO: check POSIX spec; this should probably be IO
			errors.Path(name),
			err,
		)
	}

	stat, err := ci.stat(name, ipldNode)
	if err != nil {
		return nil, errors.New(op,
			errors.Path(name),
			errors.IO, // TODO: [review] double check this Kind makes sense for this.
			err,
		)
	}
	statFn := ci.genStatFunc(name, stat)

	// XXX: Look at this arity. No way dude. Databag them at least.
	directory, err := openIPFSDir(ci.ctx, ci.core, ipldNode, statFn, ci.creationTime)
	if err != nil {
		return nil, errors.New(op,
			errors.Path(name),
			errors.IO, // TODO: [review] double check this Kind makes sense for this.
			err,
		)
	}
	return directory, nil
}

func (*coreInterface) Rename(_, _ string) error {
	const op errors.Op = "ipfscore.Rename"
	// TODO: use abstract, consistent, error values
	// (^ this means reimplementing pkg `iferrors` with new Go conventions)
	//return errReadOnly
	return errors.New(op, errors.InvalidOperation)
}
