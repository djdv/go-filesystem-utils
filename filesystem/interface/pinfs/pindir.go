package pinfs

import (
	"context"
	gopath "path"

	tcom "github.com/ipfs/go-ipfs/filesystem/interface"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	coreoptions "github.com/ipfs/interface-go-ipfs-core/options"
)

// TODO: make a pass on everything [AM] [hasty port]

// a `Directory` containing the node's pins (as a stream of entries).
type pinDirectoryStream struct {
	pinAPI coreiface.PinAPI
}

func (ps *pinDirectoryStream) SendTo(ctx context.Context, receiver chan<- tcom.PartialEntry) error {
	// get the pin stream
	pinChan, err := ps.pinAPI.Ls(ctx, coreoptions.Pin.Ls.Recursive())
	if err != nil {
		close(receiver)
		return err
	}

	// start sending translated entries to the receiver
	go translateEntries(ctx, pinChan, receiver)
	return nil
}

type pinEntryTranslator struct{ coreiface.Pin }

func (pe *pinEntryTranslator) Name() string { return gopath.Base(pe.Path().String()) }
func (pe *pinEntryTranslator) Error() error { return pe.Err() }

// TODO: review cancel semantics;
func translateEntries(ctx context.Context, pins <-chan coreiface.Pin, out chan<- tcom.PartialEntry) {
out:
	for pin := range pins {
		msg := &pinEntryTranslator{Pin: pin}

		select {
		// translate the entry and try to send it
		case out <- msg:
			if pin.Err() != nil {
				break out // exit after relaying a message with an error
			}

		// or bail if we're canceled
		case <-ctx.Done():
			break out
		}
	}
	close(out)
}
