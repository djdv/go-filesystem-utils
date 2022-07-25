package motd

import (
	goerrors "errors"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/p9p/errors"
	"github.com/hugelgupf/p9/p9"
)

// TODO: funcopts
// - Aname
func Write(client *p9.Client, filename, message string) (err error) {
	motdDir, err := client.Attach(MOTDFilename)
	if err != nil {
		return err
	}
	defer func() {
		cErr := motdDir.Close()
		if err == nil {
			err = cErr
		}
	}()
	return writeMessage(motdDir, filename, message)
}

func makeMessage(dir p9.File, filename string) (p9.File, error) {
	const flags = p9.WriteOnly
	wnames := []string{filename}
	_, messageFile, err := dir.Walk(wnames)
	if err != nil {
		err = runtimeHax(err) // see: [d9e856ce-c3ca-47be-b2e0-db5213c88c61]
		if !goerrors.Is(err, errors.ENOENT) {
			return nil, err
		}
		_, dirClone, err := dir.Walk(nil)
		if err != nil {
			return nil, err
		}
		messageFile, _, _, err := dirClone.Create(filename, flags,
			p9.ModeRegular|p9.AllPermissions, p9.NoUID, p9.NoGID)
		return messageFile, err
	}

	// TODO: check mode attr isregular
	// TODO: truncate/setattr on open

	if _, _, err := messageFile.Open(flags); err != nil {
		return nil, err
	}
	return messageFile, nil
}

// see: [d9e856ce-c3ca-47be-b2e0-db5213c88c61]
// linux.Errno is internalized upstream
func runtimeHax(err error) errors.Errno {
	// FIXME: this is extremely bad
	// proper fix upstream is necessary
	// type notError struct{ l, h unsafe.Pointer }
	// return *(*errors.Errno)((*notError)(unsafe.Pointer(&err)).h)

	// Let's not fuck around.
	return errors.Errno(reflect.ValueOf(err).Uint())
}

func writeMessage(dir p9.File, filename, message string) (err error) {
	file, err := makeMessage(dir, filename)
	if err != nil {
		return err
	}
	defer func() {
		cErr := file.Close()
		if err == nil {
			err = cErr
		}
	}()
	if _, err := file.WriteAt([]byte(message), 0); err != nil {
		return err
	}
	return nil
}
