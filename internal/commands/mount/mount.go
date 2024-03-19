package mount

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/commands/client"
	"github.com/djdv/go-filesystem-utils/internal/commands/daemon"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
	"github.com/jaevor/go-nanoid"
)

type (
	Marshaler json.Marshaler
	metadata  struct {
		permissions p9.FileMode
		uid         p9.UID
		gid         p9.GID
	}
	settings struct {
		client client.Options
		metadata
	}
	Option    func(*settings) error
	idGenFunc = func() string
)

const (
	DefaultUID         = p9.NoUID
	DefaultGID         = p9.NoGID
	DefaultPermissions = p9fs.ReadUser | p9fs.WriteUser | p9fs.ExecuteUser |
		p9fs.ReadGroup | p9fs.ExecuteGroup |
		p9fs.ExecuteOther
)

// Attach requests the file system service to attach the provided mountpoints.
func Attach(hostID filesystem.Host, guestID filesystem.ID,
	mountpoints []Marshaler, options ...Option,
) error {
	settings := settings{
		metadata: metadata{
			permissions: DefaultPermissions,
			uid:         DefaultUID,
			gid:         DefaultGID,
		},
	}
	if err := generic.ApplyOptions(&settings, options...); err != nil {
		return err
	}
	data, err := marshalMountpoints(mountpoints)
	if err != nil {
		return err
	}
	client, err := settings.client.GetClient()
	if err != nil {
		return err
	}
	metadata := settings.metadata
	return generic.CloseWithError(
		communicate(client, hostID, guestID, metadata, data),
		client,
	)
}

func marshalMountpoints(mountpoints []Marshaler) ([][]byte, error) {
	data := make([][]byte, len(mountpoints))
	for i, mountpoint := range mountpoints {
		datum, err := mountpoint.MarshalJSON()
		if err != nil {
			return nil, err
		}
		data[i] = datum
	}
	return data, nil
}

func communicate(client *p9.Client,
	hostID filesystem.Host, guestID filesystem.ID,
	metadata metadata, data [][]byte,
) error {
	root, err := client.Attach("")
	if err != nil {
		return err
	}
	guests, err := getMountPointDir(root, metadata, hostID, guestID)
	if err != nil {
		return generic.CloseWithError(
			daemon.ReceiveError(root, err),
			root,
		)
	}
	if err = writeMountPoints(guests, metadata, data); err != nil {
		err = daemon.ReceiveError(root, err)
	}
	return generic.CloseWithError(
		err,
		guests, root,
	)
}

func getMountPointDir(
	root p9.File, metadata metadata,
	hostID filesystem.Host, guestID filesystem.ID,
) (p9.File, error) {
	wnames := []string{
		string(hostID),
		string(guestID),
	}
	_, mounts, err := root.Walk([]string{daemon.MountsFileName})
	if err != nil {
		return nil, err
	}
	var (
		permissions = metadata.permissions
		uid         = metadata.uid
		gid         = metadata.gid
	)
	guests, err := p9fs.MkdirAll(mounts, wnames, permissions, uid, gid)
	return guests, generic.CloseWithError(err, mounts)
}

func writeMountPoints(guests p9.File, metadata metadata, data [][]byte) error {
	idGen, err := newIDGenerator()
	if err != nil {
		return err
	}
	// TODO: mountdir should have a clone file instead
	// of us calling create. I.e. namen should be
	// determined server-side.
	var (
		errs        []error
		permissions = metadata.permissions ^
			(p9fs.ExecuteOther | p9fs.ExecuteGroup | p9fs.ExecuteUser)
		uid = metadata.uid
		gid = metadata.gid
	)
	for _, datum := range data {
		name := fmt.Sprintf("%s.json", idGen())
		if err := newMountFile(
			guests, permissions,
			uid, gid,
			name, datum,
		); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func newIDGenerator() (idGenFunc, error) {
	const (
		idLength       = 8
		base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	)
	return nanoid.CustomASCII(base58Alphabet, idLength)
}

func newMountFile(guests p9.File,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
	name string, data []byte,
) error {
	_, guestsClone, err := guests.Walk(nil)
	if err != nil {
		return err
	}
	targetFile, _, _, err := guestsClone.Create(name, p9.WriteOnly, permissions, uid, gid)
	if err != nil {
		return generic.CloseWithError(err, guestsClone)
	}
	// NOTE: targetFile and guestClone are now aliased
	// (same fid because of `Create`; i.e. close only one of them.).
	_, err = targetFile.WriteAt(data, 0)
	return generic.CloseWithError(err, targetFile)
}

func WithPermissions(permissions p9.FileMode) Option {
	return func(settings *settings) error {
		settings.permissions = permissions
		return nil
	}
}

func WithUID(uid p9.UID) Option {
	return func(settings *settings) error {
		settings.uid = uid
		return nil
	}
}

func WithGID(gid p9.GID) Option {
	return func(settings *settings) error {
		settings.gid = gid
		return nil
	}
}

func WithClientOptions(options ...client.Option) Option {
	return func(settings *settings) error {
		settings.client = options
		return nil
	}
}
