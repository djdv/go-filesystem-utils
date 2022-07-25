package motd

import (
	goerrors "errors"
	"fmt"
	"io"

	"github.com/djdv/go-filesystem-utils/internal/p9p/errors"
	"github.com/hugelgupf/p9/p9"
)

// TODO: funcopts
// - Aname
func Read(client *p9.Client, filename string) (message string, err error) {
	motdDir, err := client.Attach(MOTDFilename)
	if err != nil {
		return "", err
	}
	defer func() {
		cErr := motdDir.Close()
		if err == nil {
			err = cErr
		}
	}()
	return readMessage(motdDir, filename)
}

func openMessage(dir p9.File, filename string) (p9.File, uint64, error) {
	var (
		wnames    = []string{filename}
		wantAttrs = p9.AttrMask{
			Mode: true,
			Size: true,
		}
	)
	_, messageFile, filled, attr, err := dir.WalkGetAttr(wnames)
	if err != nil {
		if !goerrors.Is(err, errors.ENOSYS) {
			return nil, 0, err
		}
		// Slow path.
		if _, messageFile, err = dir.Walk(wnames); err != nil {
			return nil, 0, err
		}
		if _, filled, attr, err = messageFile.GetAttr(wantAttrs); err != nil {
			return nil, 0, err
		}
	}
	if err := checkMessageAttrs(filled, attr); err != nil {
		return nil, 0, err
	}
	if _, _, err := messageFile.Open(p9.ReadOnly); err != nil {
		return nil, 0, err
	}
	return messageFile, attr.Size, nil
}

func checkMessageAttrs(filled p9.AttrMask, attr p9.Attr) error {
	// TODO: message "target file" -> $actualFilename
	if !filled.Mode {
		return fmt.Errorf("stat does not contain target file's type")
	}
	if !filled.Size {
		return fmt.Errorf("stat does not contain target file's size")
	}
	if mode := attr.Mode; !mode.IsRegular() {
		return fmt.Errorf("expected target file to be regular (mode %v) but got: %v",
			p9.ModeRegular, mode.FileType(),
		)
	}
	return nil
}

func readMessage(dir p9.File, filename string) (_ string, err error) {
	file, size, err := openMessage(dir, filename)
	if err != nil {
		return "", err
	}
	defer func() {
		cErr := file.Close()
		if err == nil {
			err = cErr
		}
	}()
	var (
		off int64
		b   = make([]byte, size)
	)
	for {
		n, err := file.ReadAt(b, off)
		off += int64(n)
		if err != nil {
			if !goerrors.Is(err, io.EOF) {
				return "", err
			}
			break
		}
	}
	return string(b[:off]), nil
}
