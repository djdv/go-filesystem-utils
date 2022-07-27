package motd

import (
	goerrors "errors"
	"fmt"
	"math"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/p9p/errors"
	"github.com/hugelgupf/p9/p9"
)

// TODO: funcopts
// - Aname
func List(client *p9.Client) (_ p9.Dirents, err error) {
	motdDir, err := client.Attach(MOTDFilename)
	if err != nil {
		return nil, err
	}
	defer func() {
		cErr := motdDir.Close()
		if err == nil {
			err = cErr
		}
	}()
	return listFiles(motdDir)
}

func openList(dir p9.File) (p9.File, error) {
	wantAttrs := p9.AttrMask{Mode: true}
	_, dirClone, filled, attr, err := dir.WalkGetAttr(nil)
	if err != nil {
		if !goerrors.Is(err, errors.ENOSYS) {
			return nil, err
		}
		// Slow path.
		if _, dirClone, err = dir.Walk(nil); err != nil {
			return nil, err
		}
		if _, filled, attr, err = dirClone.GetAttr(wantAttrs); err != nil {
			return nil, err
		}
	}
	if err := checkListAttrs(filled, attr); err != nil {
		return nil, err
	}
	if _, _, err := dirClone.Open(p9.ReadOnly); err != nil {
		return nil, err
	}
	return dirClone, nil
}

func checkListAttrs(filled p9.AttrMask, attr p9.Attr) error {
	// TODO: message "target file" -> $actualFilename
	if !filled.Mode {
		return fmt.Errorf("stat does not contain target file's type")
	}
	if mode := attr.Mode; !mode.IsDir() {
		return fmt.Errorf("expected target file to be directory (mode %v) but got: %v",
			p9.ModeDirectory, mode.FileType(),
		)
	}
	return nil
}

func listFiles(dir p9.File) (_ p9.Dirents, err error) {
	listDir, err := openList(dir)
	if err != nil {
		return nil, err
	}
	defer func() {
		cErr := listDir.Close()
		if err == nil {
			err = cErr
		}
	}()
	var (
		offset uint64
		ents   p9.Dirents
	)
	for { // TODO: [Ame] double check correctness (offsets and that)
		entBuf, err := listDir.Readdir(offset, math.MaxUint32)
		if err != nil {
			return nil, err
		}
		bufferedEnts := len(entBuf)
		if bufferedEnts == 0 {
			break
		}
		offset = entBuf[bufferedEnts-1].Offset
		ents = append(ents, entBuf...)
	}
	return ents, nil
}

// TODO: remove verbose? Only useful for debugging
// TODO: reconsider name; maybe fine but hasty/placeholder.
func FormatList(ents p9.Dirents, verbose bool) string {
	const (
		headDecorator   = '🌱'
		middleDecorator = '├'
		tailDecorator   = '└'
	)
	var (
		sb        strings.Builder
		decorator = headDecorator
		print     = func(ent p9.Dirent) {
			if verbose {
				sb.WriteString(fmt.Sprintf("%c {v%d}%s\n", decorator, ent.QID.Version, ent.Name))
			} else {
				sb.WriteString(fmt.Sprintf("%c %s\n", decorator, ent.Name))
			}
		}
	)
	sb.WriteString(fmt.Sprintf("%c\n", decorator))

	entsEnd := len(ents)
	if entsEnd == 0 {
		decorator = tailDecorator
		print(p9.Dirent{Name: "empty"})
		return sb.String()
	}
	entsEnd--

	decorator = middleDecorator
	for _, ent := range ents[:entsEnd] {
		print(ent)
	}

	decorator = tailDecorator
	print(ents[entsEnd])
	return sb.String()
}
