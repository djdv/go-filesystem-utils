package commands

import (
	"encoding/json"
	"errors"
	"flag"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
)

type (
	// TODO: can we consolidate mountpoint?
	// used here and in daemon.
	// fields must be synonymous.
	// better if they're actually the same.
	//
	// XXX: this jawn ugly; but it probably has to be.
	guestCommandConstraint[T any] interface {
		*T
		command.FlagBinder
	}
	mountPointSettings[
		HT, GT any,
		H hostCommandConstraint[HT],
		G guestCommandConstraint[GT],
	] struct {
		// Host  H
		// Guest G
		Host  HT
		Guest GT
		clientSettings
	}
)

func (mp *mountPointSettings[HT, GT, H, G]) BindFlags(flagSet *flag.FlagSet) {
	mp.clientSettings.BindFlags(flagSet)
	// HACK: we need a better way to do this.
	// mp.Host = new(HT)
	// mp.Guest = new(GT)
	// mp.Host.BindFlags(flagSet)
	// mp.Guest.BindFlags(flagSet)
	H(&mp.Host).BindFlags(flagSet)
	G(&mp.Guest).BindFlags(flagSet)
}

// TODO: name + move to flags; quick hacks for testing.
type lazyInterface interface {
	lazy() error
}

func (mp *mountPointSettings[HT, GT, H, G]) lazy() error {
	if lazy, ok := any(&mp.Host).(lazyInterface); ok {
		if err := lazy.lazy(); err != nil {
			return err
		}
	}
	if lazy, ok := any(&mp.Guest).(lazyInterface); ok {
		if err := lazy.lazy(); err != nil {
			return err
		}
	}
	return nil
}

func (mp *mountPointSettings[HT, GT, H, G]) setTarget(target string) error {
	switch typed := any(mp.Host).(type) {
	case *cgofuse.MountPoint:
		typed.Point = target
	default:
		// TODO: real message
		return errors.New("unexpected type")
	}
	return nil
}

func (mp *mountPointSettings[HT, GT, H, G]) marshalMountpoint() ([]byte, error) {
	return json.Marshal(struct {
		Host  HT
		Guest GT
	}{
		Host:  mp.Host,
		Guest: mp.Guest,
	})
	/* TODO lint;
	We changed format and likely won't need this anymore.
	// golang/go
	// - #6213 can't inline JSON fields.
	// - #49030 embedding type parameters is forbidden.
	// HACK: lol byte splice.
	host, err := json.Marshal(mp.Host)
	if err != nil {
		return nil, err
	}
	guest, err := json.Marshal(mp.Guest)
	if err != nil {
		return nil, err
	}
	// TODO: [Ame] Volgate et vulgar de humanitas.
	// Modern and prudent Ænglisc please.
	// (No removal of feet|heads; no Pennsylvania English.)
	//
	// We'll replace `}{` with `,`,
	// by depeditating the host
	// and decapitating the guest;
	// binding betwixt with substitute.
	const substitute = ','
	var (
		buf      bytes.Buffer
		hostEnd  = len(host) - 1
		guestEnd = len(guest)
		size     = hostEnd + guestEnd
	)
	buf.Grow(size)
	buf.Write(host[:hostEnd])
	buf.WriteByte(substitute)
	buf.Write(guest[1:])
	return buf.Bytes(), nil
	*/
}
