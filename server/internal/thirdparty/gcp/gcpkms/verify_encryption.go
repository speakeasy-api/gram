package gcpkms

import (
	"bytes"
	"context"
	"errors"
)

// encryptionProbeAAD is the associated data the encryption probe binds, so the
// probe also proves the key honours associated data.
const encryptionProbeAAD = "gram-key-verification-probe-aad"

// VerifyEncryptionKey proves end to end that a client can reach a symmetric
// key AND use it: it encrypts ProbePayload with associated data and decrypts
// the result again.
//
// Nothing is persisted. Two KMS calls are made, both real cryptographic
// operations that land in the key owner's audit log, which is why the payload
// names itself.
func VerifyEncryptionKey(ctx context.Context, client EncryptionClient, keyName string) VerifyResult {
	encrypted, err := client.Encrypt(ctx, keyName, []byte(ProbePayload), []byte(encryptionProbeAAD))
	if err != nil {
		return failedVerify(ReasonForError(err), "", err)
	}

	plaintext, err := client.Decrypt(ctx, keyName, encrypted.Ciphertext, []byte(encryptionProbeAAD))
	if err != nil {
		return failedVerify(ReasonForError(err), "", err)
	}

	if !bytes.Equal(plaintext, []byte(ProbePayload)) {
		return failedVerify(ReasonRoundTripFailed, "", errors.New("decrypting the probe ciphertext did not return the probe payload"))
	}

	return VerifyResult{Verified: true, Reason: ReasonVerified, Algorithm: "", Detail: ""}
}
