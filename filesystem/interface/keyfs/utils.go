package keyfs

import (
	"context"
	"fmt"
	"strings"

	"github.com/ipfs/go-ipfs/filesystem"
	interfaceutils "github.com/ipfs/go-ipfs/filesystem/interface"
	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	coreoptions "github.com/ipfs/interface-go-ipfs-core/options"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

func noop() {} // we want this statically assigned instead of constructed where needed

// returns the appropriate fs based on the path
// along with the associated key (if any)
// and the (potentially modified) operation argument (the path string for the selected/target `Interface`).
func (ki *keyInterface) selectFS(path string) (fs filesystem.Interface, coreKey coreiface.Key, fsPath string, deferFunc func(), err error) {
	deferFunc = noop

	if path == "/" {
		fs = ki
		fsPath = path
		return
	}

	keyName, remainder := splitPath(path)

	if coreKey, err = ki.checkKey(keyName); err != nil {
		err = iferrors.Other(path, err)
		return
	}

	if coreKey != nil { // we own this key; operate on it
		if remainder != "" { // if the path contains a subroot we can assume MFS
			fs, err = ki.getRoot(coreKey)
			fsPath = remainder
			deferFunc = func() { fs.Close() }
			return
		}

		callCtx, cancel := interfaceutils.CallContext(ki.ctx)
		defer cancel()
		// if there is no subpath, we can't assume this requests destination
		// so check its type to determine the FS for it (Files, Links: KeyFS, Directories: MFS)
		var stat *filesystem.Stat
		if stat, _, err = ki.core.Stat(callCtx, coreKey.Path(), filesystem.StatRequest{Type: true}); err != nil {
			err = iferrors.IO(path, err)
			return
		}

		switch t := stat.Type; t {
		case coreiface.TFile, coreiface.TSymlink:
			fs = ki
			fsPath = path
		case coreiface.TDirectory:
			fs, err = ki.getRoot(coreKey)
			fsPath = remainder
			deferFunc = func() { fs.Close() }
		default:
			err = iferrors.Other(path,
				fmt.Errorf("unexpected type: %v", t))
		}

		return
	}

	// otherwise proxy the path literally to the core
	fsPath = path
	fs = ki.ipns
	return
}

// we publish offline just to initialize the key in the node's context
// the world update is not our concern; we just want to be fast locally
// the caller should make a globabl broadcast if they want to sync with the wired
func localPublish(ctx context.Context, core coreiface.CoreAPI, keyName string, target corepath.Path) error {
	oAPI, err := core.WithOptions(coreoptions.Api.Offline(true))
	if err != nil {
		return iferrors.Other(keyName, err)
	}

	if _, err = oAPI.Name().Publish(ctx, target, coreoptions.Name.Key(keyName), coreoptions.Name.AllowOffline(true)); err != nil {
		return iferrors.Other(keyName, err)
	}

	return nil
}

func splitPath(path string) (key, remainder string) {
	slashIndex := 1 // skip leading slash
	slashIndex += strings.IndexRune(path[1:], '/')

	if slashIndex == 0 { // input looks like: `/key`
		key = path[1:]
	} else { // input looks like: `/key/sub...`
		key = path[1:slashIndex]
		remainder = path[slashIndex:]
	}
	return
}

// caller should expect key to be nil if not found, with err also being nil
func (ki *keyInterface) checkKey(keyName string) (coreiface.Key, error) {
	callContext, cancel := interfaceutils.CallContext(ki.ctx)
	defer cancel()

	keys, err := ki.core.Key().List(callContext)
	if err != nil {
		return nil, err
	}

	for _, key := range keys {
		if key.Name() == keyName {
			return key, nil
		}
	}

	// not having a key is not an error
	return nil, nil
}
