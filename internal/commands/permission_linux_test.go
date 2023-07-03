//go:build linux

package commands

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPermissionSymbolizer(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		string
		fs.FileMode
	}{
		{
			FileMode: 0o777 | fs.ModeSetuid,
			string:   "u=rwxs,g=rwx,o=rwx",
		},
		{
			FileMode: 0o777 | fs.ModeSetgid,
			string:   "u=rwx,g=rwxs,o=rwx",
		},
		{
			FileMode: 0o777 | fs.ModeSticky,
			string:   "u=rwx,g=rwx,o=rwxt",
		},
		{
			FileMode: 0o777 | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky,
			string:   "u=rwxs,g=rwxs,o=rwxt",
		},
		{
			FileMode: 0o751,
			string:   "u=rwx,g=rx,o=x",
		},
		{
			FileMode: 0o704,
			string:   "u=rwx,o=r",
		},
	} {
		var (
			mode = test.FileMode
			got  = modeToSymbolicPermissions(mode)
			want = test.string
		)
		if got != want {
			t.Errorf("unexpected symbolic representation of \"%o\""+
				"\n\tgot: %s"+
				"\n\twant: %s",
				mode, got, want,
			)
		}
	}
}

func TestParsePOSIXPermissions(t *testing.T) {
	t.Parallel()
	t.Run("valid", parsePOSIXPermissionsValid)
	t.Run("invalid", parsePOSIXPermissionsInvalid)
}

func parsePOSIXPermissionsValid(t *testing.T) {
	t.Parallel()
	var (
		testDir       = t.TempDir()
		testFile, err = os.CreateTemp(testDir, "permission-test-file")
		fileName      = testFile.Name()
	)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(fileName)
	fileStat, err := testFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	fileMode := fileStat.Mode()

	dirPath := filepath.Join(testDir, "permission-test-dir")
	if err := os.Mkdir(dirPath, 0o751); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(dirPath)
	dirStat, err := os.Stat(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	dirMode := dirStat.Mode()
	clausesList := []string{
		"0",
		"644",
		"755",
		"777",
		"01751",
		"02751",
		"04751",
		"07777",
		"=rwx",
		"a+",
		"a+=",
		"++w",
		"--w",
		"a++w",
		"a--w",
		"go+-w",
		"g=o-w",
		"g-r+w",
		"uo=g",
		"u+rwx",
		"u=rw,g=rx,o=",
		"a-rwx,u=rw,g+x,o-rw",
		"u+r,g+w,o+x",
		"u-w,g-r,o-x",
		"u=rwx,g=rx,o=r",
		"u+rw,g=x,o-w",
		"u-w,g=r,o+x",
		"u=r,g+w,o-r",
		"0754",
		"o=u-g",
		"=",
		"=X",
		"777",
		"=X",
		"a=",
		"u=x",
		"g=X",
		"u=s",
		"g=s",
		"o=s",
		"a=s",
		"=",
		"g=s",
		"=xt",
	}
	const utilityName = `chmod`
	compare := func(mode fs.FileMode, targetName, clauses string, cmdArgs []string) (fs.FileMode, error) {
		stdio, err := exec.Command(utilityName, cmdArgs...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, stdio)
		}
		info, err := os.Stat(targetName)
		if err != nil {
			t.Fatal(err)
		}
		want := info.Mode()
		got, err := parsePOSIXPermissions(mode, clauses)
		if err != nil {
			return want, fmt.Errorf("\"%s\": %v", clauses, err)
		}
		if got != want {
			return want, fmt.Errorf("unexpected permissions for clause(s) \"%s\""+
				"\n\tinitial: %s"+
				"\n\tgot: %s"+
				"\n\twant: %s",
				clauses, mode, got, want,
			)
		}
		return want, nil
	}
	for _, clauses := range clausesList {
		// NOTE: "--" is only required for portability.
		// Systems that parse arguments with something like `getopt` (e.g. GNU)
		// require this, while most SYSV implementations (e.g. Illumos) do not.
		cmdArgs := []string{"--", clauses, fileName}
		wantFile, err := compare(fileMode, fileName, clauses, cmdArgs)
		fileMode = wantFile
		if err != nil {
			t.Error(err)
		}
		cmdArgs[len(cmdArgs)-1] = dirPath
		wantDir, err := compare(dirMode, dirPath, clauses, cmdArgs)
		dirMode = wantDir
		if err != nil {
			t.Error(err)
		}
	}
}

func parsePOSIXPermissionsInvalid(t *testing.T) {
	t.Parallel()
	clausesList := []string{
		"invalid",
		"🔐",
		"u=🔐",
		"u+x, o+x",
		"u+x,,o+x",
		"u+abc",
		"j=rwx",
		"u?rwx",
		"u=go",
		"888",
		"0888",
		"123456",
	}
	for _, clauses := range clausesList {
		got, err := parsePOSIXPermissions(0, clauses)
		if err == nil {
			t.Errorf(
				"expected error for clause(s) \"%s\" but got mode: %s",
				clauses, got,
			)
		}
	}
}
