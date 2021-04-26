package service

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/adrg/xdg"
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"golang.org/x/sys/windows"
)

var (
	DelayedAutoStartParameter = fscmds.CmdsParameterSet{
		Name:        "DelayedAutoStart",
		Description: "Prevent the service from starting immediately after booting.",
		Environment: "FS_SERVICE_DELAY",
	}
	ServicePasswordParameter = fscmds.CmdsParameterSet{
		Name:        "Password",
		Description: "Password to use when interfacing with the system service manager.",
		Environment: "FS_SERVICE_PASSWORD",
	}
)

var servicePlatformOptions = platformOptions{
	stringOptions: []fscmds.CmdsParameterSet{
		ServicePasswordParameter,
	},
	boolOptions: []fscmds.CmdsParameterSet{
		DelayedAutoStartParameter,
	},
}

/* NOTE: [MSDN]
"If an application requires normal Users to have write access to an application
specific subdirectory of CSIDL_COMMON_APPDATA,
then the application must explicitly modify the security
on that sub-directory during application setup."

WSA currently (v19043.928) requires
read, write, and delete for unix domain sockets to `connect`.
We want 'Users' to be able to do that, so we allow that access
on files underneath the service directory.
*/
func prepareSystemSocket() (socketPath string, cleanup func() error, err error) {
	var (
		systemSid          *windows.SID
		dacl               *windows.ACL
		securityAttributes *windows.SecurityAttributes
		pszServiceDir      *uint16
		serviceDir         = filepath.Join(
			xdg.ConfigDirs[len(xdg.ConfigDirs)-1],
			fscmds.ServiceName,
		)
	)
	if systemSid, err = windows.CreateWellKnownSid(windows.WinLocalSystemSid); err != nil {
		return
	}
	if dacl, err = getServiceSecurityTemplate(systemSid); err != nil {
		return
	}
	if securityAttributes, err = getServiceSecurityAttributes(systemSid, dacl); err != nil {
		return
	}
	if pszServiceDir, err = windows.UTF16PtrFromString(serviceDir); err != nil {
		return
	}

	if err = windows.CreateDirectory(pszServiceDir, securityAttributes); err != nil {
		if errors.Is(err, fs.ErrExist) {
			// Don't remake dir,
			// but set its security.
			err = windows.SetNamedSecurityInfo(serviceDir, windows.SE_FILE_OBJECT,
				windows.DACL_SECURITY_INFORMATION,
				systemSid, systemSid,
				dacl, nil)
		}
	}
	if err != nil {
		return
	}

	socketPath = filepath.Join(serviceDir, fscmds.ServerName)
	cleanup = func() error { return os.Remove(serviceDir) }

	return
}

func getServiceSecurityTemplate(systemSid *windows.SID) (*windows.ACL, error) {
	usersSid, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	if err != nil {
		return nil, err
	}
	aces := []windows.EXPLICIT_ACCESS{
		{ // recursive ALL for systemSid
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(systemSid),
			},
		},
		{ // level 1 files (/service-dir/*), such as the unix socket
			AccessPermissions: windows.GENERIC_READ |
				windows.GENERIC_WRITE |
				windows.DELETE,
			AccessMode: windows.GRANT_ACCESS,
			Inheritance: windows.OBJECT_INHERIT_ACE |
				windows.INHERIT_ONLY_ACE | // does not apply to the container (level 0)
				windows.INHERIT_NO_PROPAGATE, // level 2+ does not get this
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(usersSid),
			},
		},
	}
	return windows.ACLFromEntries(aces, nil)
}

func getServiceSecurityAttributes(systemSid *windows.SID, dacl *windows.ACL) (*windows.SecurityAttributes, error) {
	sd, err := windows.NewSecurityDescriptor()
	if err != nil {
		return nil, err
	}

	adminSid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, err
	}

	if err := sd.SetDACL(dacl, true, false); err != nil {
		return nil, err
	}
	if err := sd.SetOwner(systemSid, false); err != nil {
		return nil, err
	}
	if err := sd.SetGroup(adminSid, false); err != nil {
		return nil, err
	}

	securityAttributes := new(windows.SecurityAttributes)
	securityAttributes.Length = uint32(unsafe.Sizeof(*securityAttributes))
	securityAttributes.SecurityDescriptor = sd

	return securityAttributes, nil
}
