# Internal session issuer keys

An organization's internal session issuer signs enterprise-managed authorization assertions (ID-JAGs) for its users. Vendors trust its issuer URL, `<origin>/oauth/idp/org/<org-id>`, which never changes. Its signing keys live in GCP KMS on the platform identity provider key ring (`GRAM_IDENTITY_PROVIDER_KMS_KEY_RING`), named `emaissuer-*`.

## Suspected key compromise

1. Read the issuer (`internalSessionIssuers.get`) and note the active `kid`.
2. Call `internalSessionIssuers.rotate` with `compromised: true` and that `active_kid`, as an organization admin. It runs in three steps. Only the first, containment, is independent of KMS: it needs just the admin role and the database, never the rollout flag, the entitlement or the rate limiter. KMS cleanup and the replacement need KMS, and the replacement also needs the mint rate limiter, so either can stay pending:
   1. **Contain.** One transaction revokes the active, pending and retired keys and withdraws them from the JWKS. The issuer is now `keyless`: it signs nothing. Verifiers drop the old keys within about two minutes (two 60-second cache windows).
   2. **Clean up KMS.** Each revoked key's KMS version is disabled and the signer's grant removed. The grant is removed for the service account the key's credential impersonated, or for the configured platform signer if that credential is gone. A key whose step fails, or whose grant has no known account, stays recorded as pending (`json_web_keys.kms_disabled_at` is NULL), and the response reports it in `kms_cleanup_pending`. So `kms_cleanup_pending: 0` means every version is disabled and every grant removed.
   3. **Replace.** A fresh key is created and activated. The issuer URL is unchanged.
3. Read the response:
   - `state: enabled` and `kms_cleanup_pending: 0`: done.
   - `kms_cleanup_pending` above 0: the compromised keys are already unpublished. The hourly sweep retries cleanup until it succeeds. Watch `gram.internal_session_issuer.kms_cleanup.pending`, or clean up by hand (below).
   - `state: keyless`: no replacement yet (KMS outage, signing misconfiguration, or the mint rate limit is exhausted or unreachable, since minting fails closed). Nothing is signed meanwhile, and EMA connections fall back to other identity providers. Repeat the call; with no active key, `active_kid` may be left out.
4. Repeats that name a key that was already replaced change nothing, so retries never revoke the fresh key or mint further keys.
5. If the platform signing identity itself is suspect, pause all signing first (below), then rotate every affected organization.
6. Check the `gram.internal_session_issuer.operations` metric (`gram.internal_session_issuer.operation=rotate_compromised`) for failed steps.

## Pausing all signing

- Without a restart: enable the PostHog flag `internal-session-issuer-signing-paused`, for everyone or for one organization's group. Discovery keeps serving.
- The switch fails closed. Signing runs only while that flag exists and evaluates to off. A missing or deleted flag, or an unavailable flag provider, pauses signing. Keep the flag defined, and off, in every environment.
- At startup: set `GRAM_INTERNAL_SESSION_ISSUER_SIGNING_PAUSED=true` on the `start` deployment. A deployment that signs must register the same flag.

An organization admin can also stop their own organization's signing with `internalSessionIssuers.setEnabled` and `enabled: false`. Like compromised rotation, this needs only the admin role and the database.

## Manual KMS cleanup

Revoked and expired keys have their KMS versions disabled and the signer's grant removed automatically: on rotation, on management reads, and by the hourly `internal-session-issuer-key-sweep` schedule, which runs on the shared task queue only. Each run sweeps in pages of about 8 minutes, resuming by organization id, for up to three pages; anything left is swept next hour. Each revoked key stays pending (`json_web_keys.kms_disabled_at` NULL) until both steps succeed, including when no service account is known for its grant (neither the key's credential nor `GRAM_IDENTITY_PROVIDER_SIGNING_SERVICE_ACCOUNT`), and the sweep reports the count still pending as `gram.internal_session_issuer.kms_cleanup.pending`. A value that stays above 0 across sweeps needs a look. To clean up by hand:

```sh
gcloud kms keys versions disable <version> --key <key> --keyring <ring> --location <location> --project <project>
gcloud kms keys remove-iam-policy-binding <key> --keyring <ring> --location <location> --project <project> \
  --member serviceAccount:<signing service account> --role roles/cloudkms.signerVerifier
```

Never destroy a version a live row still references. The next sweep re-runs both steps, which is safe on an already disabled version, and then records the cleanup.

## Public discovery traffic

The discovery and JWKS routes are unauthenticated. Each process caches up to 4,096 documents, including "not found", for 10 seconds, and concurrent misses for one document share a single read. Metadata routes read only the issuer row. At most 16 distinct documents are read at once per process, each with a 2-second deadline; a miss beyond that gets a 503 with `Retry-After: 1` instead of waiting on the database pool. A cached document's `max-age` is reduced by its time in the cache, so a verifier never holds a key longer than 60 seconds after this process read it, and the activation and overlap timings still hold.

So repeated requests for the same document reach the database about once per process per 10 seconds. Requests spread over many distinct organization ids still read once per id, and can evict each other once there are more than 4,096. Volumetric abuse is left to edge rate limiting on `/oauth/idp/org/*` and the matching `/.well-known/*` paths. Add an edge rule there if traffic grows.

## IAM assumptions

- The provisioning identity can create keys on the identity provider key ring and set their IAM policy.
- The platform signing service account (`GRAM_IDENTITY_PROVIDER_SIGNING_SERVICE_ACCOUNT`) holds `roles/cloudkms.signerVerifier` on each issuer key, granted per key at creation and removed at retirement. It shares the ring with Okta connection keys; the `emaissuer-` prefix is the only separation.
