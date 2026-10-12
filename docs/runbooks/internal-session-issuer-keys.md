# Internal session issuer keys

An organization's internal session issuer signs enterprise-managed authorization assertions (ID-JAGs) for its users. Vendors trust its issuer URL, `<origin>/oauth/idp/org/<org-id>`, which never changes. Its signing keys live in GCP KMS on the platform identity provider key ring (`GRAM_IDENTITY_PROVIDER_KMS_KEY_RING`), named `emaissuer-*`.

## Suspected key compromise

1. Replace every key at once: `internalSessionIssuers.rotate` with `compromised: true`, as an organization admin. It revokes the active, pending and retired keys, disables their KMS versions, and activates a fresh key. The URL is unchanged; verifiers drop the old keys within about two minutes (two 60-second cache windows).
2. If the platform signing identity itself is suspect, pause all signing first (below), then rotate every affected organization.
3. Check the `gram.internal_session_issuer.operations` metric (`operation=rotate_compromised`) and the logs for any "disable it by hand" error.

## Pausing all signing

- Without a restart: enable the PostHog flag `internal-session-issuer-signing-paused`, for everyone or for one organization's group. Discovery keeps serving.
- At startup: set `GRAM_INTERNAL_SESSION_ISSUER_SIGNING_PAUSED=true` on the `start` and `mcp` deployments.

An organization admin can also stop their own organization's signing with `internalSessionIssuers.setEnabled` and `enabled: false`.

## Manual KMS cleanup

Revoked and expired keys have their KMS versions disabled and the signer's grant removed automatically: on rotation, on management reads, and by the hourly `internal-session-issuer-key-sweep` schedule. When a log line says to disable a version or remove a grant by hand:

```sh
gcloud kms keys versions disable <version> --key <key> --keyring <ring> --location <location> --project <project>
gcloud kms keys remove-iam-policy-binding <key> --keyring <ring> --location <location> --project <project> \
  --member serviceAccount:<signing service account> --role roles/cloudkms.signerVerifier
```

Never destroy a version a live row still references.

## IAM assumptions

- The provisioning identity can create keys on the identity provider key ring and set their IAM policy.
- The platform signing service account (`GRAM_IDENTITY_PROVIDER_SIGNING_SERVICE_ACCOUNT`) holds `roles/cloudkms.signerVerifier` on each issuer key, granted per key at creation and removed at retirement. It shares the ring with Okta connection keys; the `emaissuer-` prefix is the only separation.
