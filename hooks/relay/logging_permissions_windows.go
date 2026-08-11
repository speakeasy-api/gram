//go:build windows

package relay

import (
	"os"

	"golang.org/x/sys/windows"
)

func secureLogFile(file *os.File) error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return err
	}
	user, err := token.GetTokenUser()
	closeErr := token.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, nil)
	if err != nil {
		return err
	}

	// SetNamedSecurityInfo opens the file with WRITE_DAC itself. The append
	// handle returned by os.OpenFile intentionally does not carry that access.
	return windows.SetNamedSecurityInfo(
		file.Name(),
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	)
}
