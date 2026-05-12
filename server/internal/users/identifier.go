package users

import "github.com/google/uuid"

var speakeasyNamespace = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("speakeasy.com"))

func UserIDFromWorkOSID(workosID string) string {
	return uuid.NewSHA1(speakeasyNamespace, []byte(workosID)).String()
}
