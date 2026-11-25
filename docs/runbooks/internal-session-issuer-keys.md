# Internal session issuer keys

An organization's internal session issuer signs enterprise-managed authorization assertions (ID-JAGs) for its users. Vendors trust its issuer URL, `<origin>/oauth/idp/org/<org-id>`, which never changes. Its signing keys live in GCP KMS on the platform identity provider key ring (`GRAM_IDENTITY_PROVIDER_KMS_KEY_RING`), named `emaissuer-*`. Each assertion's email must be on one of the organization's verified domains at signing time, since vendors trust the email alone.

Speakeasy serves a vendor only where an admin added it to that vendor's EMA provider order, and only while the organization has both the `internal-session-issuer` rollout flag and the `sign-in-enforcement` flag (from the sign-in linking change, #7460) on.

## Issuer origin

`<origin>` is the outbound callback origin (`GRAM_OUTBOUND_CALLBACK_URL`, defaulting to the server URL). The `start` and `worker` deployments must set the same value: the server signs and serves discovery, and the worker sweeps keys. Each issuer pins the origin it was created on, and a deployment refuses to sign, provision keys, rotate routinely or clean up KMS for an issuer pinned to another origin. Disabling and compromise containment still work, since they only touch the database, so never skip them during an origin mismatch. Changing the outbound origin makes every existing issuer unable to sign (`state: unavailable`); it does not move them, because vendors trust the old URL. To recover, set `GRAM_OUTBOUND_CALLBACK_URL` back to the origin the issuers were created on, on both deployments. Moving issuers to a new origin means each organization re-registering a new URL with every vendor, which this runbook doesn't cover.

## Suspected key compromise

An organization admin replaces a compromised key on IDP and SSO › Identity providers › Speakeasy, under **Signing keys** (**Replace compromised key**). The page calls `internalSessionIssuers.rotate` with `compromised: true` and the active `kid`; the steps below describe what it does and how to drive it through the API.

1. Read the issuer (`internalSessionIssuers.get`) and note the active `kid`.
2. Call `internalSessionIssuers.rotate` with `compromised: true` and that `active_kid`, as an organization admin. It runs in three steps. Only the first, containment, is independent of KMS: it needs just the admin role and the database, never the rollout flag, verified domains or the rate limiter. KMS cleanup and the replacement need KMS, and the replacement also needs the mint rate limiter, so either can stay pending:
   1. **Contain.** One transaction revokes the active, pending and retired keys and withdraws them from the JWKS. The issuer is now `keyless`: it signs nothing. Verifiers drop the old keys within about two minutes (two 60-second cache windows).
   2. **Clean up KMS.** Each revoked key's KMS version is disabled and the signer's grant removed, independently, each key with its own deadline. A version already disabled, scheduled for destruction or destroyed counts as disabled. The grant is removed from the service account the key's credential impersonated, read even when the credential has since been deleted. A platform credential's service account can't change while any issuer key still holds a grant made with it, and the credential can't be deleted while a live issuer key signs through it. A key whose step fails, or whose grant has no known account, stays recorded as pending (`json_web_keys.kms_disabled_at` is NULL), and the response reports it in `kms_cleanup_pending`. So `kms_cleanup_pending: 0` means every version is disabled and every grant removed.
   3. **Replace.** A fresh key is created and activated, only while the issuer is rolled out and the organization has a verified domain; otherwise the issuer stays `keyless` until both hold and an admin finishes the setup. The issuer URL is unchanged.
3. Read the response:
   - `state: enabled` and `kms_cleanup_pending: 0`: done.
   - `kms_cleanup_pending` above 0: the compromised keys are already unpublished. The hourly sweep retries cleanup until it succeeds. Watch `gram.internal_session_issuer.kms_cleanup.pending`, or clean up by hand (below).
   - `state: keyless` with `replacement_error`: no replacement yet (KMS outage, signing misconfiguration, or the mint rate limit is exhausted or unreachable, since minting fails closed). The page shows the error instead of success. Nothing is signed meanwhile: Speakeasy is skipped, and services whose EMA provider order names another provider that can serve try that provider (if its exchange fails, the request fails with an authorization error); only services with no provider able to serve get the interactive sign-in. The page offers **Finish setting up a new key** under **Signing keys**, which repeats the call; through the API, with no active key, `active_kid` may be left out.
4. Repeats that name a key that was already replaced change nothing, so retries never revoke the fresh key or mint further keys.
5. If the platform signing identity itself is suspect (the signing service account or its credential, not one organization's key), rotating alone doesn't help: whoever holds the identity can sign directly with KMS. In order:
   1. Pause all signing (below).
   2. Contain the identity in GCP: disable the signing service account or remove its keys and impersonation grants, so nothing outside Speakeasy can use it.
   3. Verify direct KMS access is cut: an `asymmetric-sign` with the old identity against an issuer key must fail.
   4. Create a new platform credential for a clean signing service account and switch Speakeasy to it (`GRAM_IDENTITY_PROVIDER_SIGNING_CREDENTIAL_ID` and `GRAM_IDENTITY_PROVIDER_SIGNING_SERVICE_ACCOUNT`). Don't re-point the old credential: its service account can't change while issuer keys hold grants made with it. Then have each affected organization's admin choose **Replace compromised key** under **Signing keys** on the Speakeasy provider page, or rotate with `compromised: true` through the API. Until AIM-556 ships a staff bulk re-key, recovery needs each org admin to click **Replace compromised key** after the credential switch.
   5. Resume signing.
6. Check the `gram.internal_session_issuer.operations` metric (`gram.internal_session_issuer.operation=rotate_compromised`) for failed steps.

## Pausing all signing

- Without a restart: enable the PostHog flag `internal-session-issuer-signing-paused`, for everyone or for one organization's group. Discovery keeps serving.
- The switch fails closed. Signing runs only while that flag exists and evaluates to off from flag definitions refreshed within the last 150 seconds. Freshness is tracked only with local evaluation (`POSTHOG_PERSONAL_API_KEY`); without it each check is a live PostHog call. A missing or deleted flag, an unavailable flag provider, or definitions the provider stopped refreshing pause signing, so a pause set during a flag provider outage still takes effect. Refused signatures count in `gram.internal_session_issuer.operations` with `gram.internal_session_issuer.pause_reason` (`startup`, `flag_on`, `indeterminate`, `stale_definitions` or `no_freshness`). Keep the flag defined, and off, in every environment.
- At startup: set `GRAM_INTERNAL_SESSION_ISSUER_SIGNING_PAUSED=true` on both the `start` and `worker` deployments. The worker evaluates the same gate before it activates a rotated key. A deployment that signs or activates keys must register the same flag.

An organization admin can also stop their own organization's signing with `internalSessionIssuers.setEnabled` and `enabled: false`. Like compromised rotation, this needs only the admin role and the database.

## Manual KMS cleanup

The same sweep finishes routine rotations: a pending key past the activation delay is activated through the same locked path as a second `rotate` call, audited as the system, and counted in `gram.internal_session_issuer.key_activations`. It skips any issuer that couldn't sign right now (rollout off, no verified domain, disabled issuer or organization, paused, or failing the KMS ring or signing identity checks), and the key waits; the gates are checked again under the issuer lock just before activating. An issuer pinned to another deployment's origin is skipped with a warning naming the organization. A pending key whose publication committed but was never observed is observed by the sweep and activated a full activation delay later. A compromise call naming a key the sweep revoked still contains its successor, since that key was active before the compromise was reported.

Revoked and expired keys have their KMS versions disabled and the signer's grant removed automatically: on rotation and by the hourly `internal-session-issuer-key-sweep` schedule, which runs on the shared task queue only. Reading the issuer never does. Each run sweeps in pages of about 5 minutes, each organization getting at most 2 of them, resuming by organization id, for up to three pages, and the next run resumes where it stopped; it starts over only after reaching the last organization, so organizations that keep failing can't starve the rest. Within an organization, keys whose cleanup keeps failing are retried after the others, so they can't hold the rest back. Sweep failures are logged per organization with its id. Each revoked key stays pending (`json_web_keys.kms_disabled_at` NULL) until both steps succeed, including when the key's credential names no service account for its grant, and the sweep reports the count still pending as `gram.internal_session_issuer.kms_cleanup.pending`. A value that stays above 0 across sweeps needs a look. To clean up by hand:

```sh
gcloud kms keys versions disable <version> --key <key> --keyring <ring> --location <location> --project <project>
gcloud kms keys remove-iam-policy-binding <key> --keyring <ring> --location <location> --project <project> \
  --member serviceAccount:<signing service account> --role roles/cloudkms.signerVerifier
```

Never destroy a version a live row still references. The next sweep re-runs both steps, which is safe on an already disabled version, and then records the cleanup.

## Public discovery traffic

The discovery and JWKS routes are unauthenticated. An organization id that isn't 1 to 128 letters, digits, `_` or `-` gets a 404 before any cache or database read. Each process caches up to 4,096 documents for 10 seconds, and "not found" answers separately, up to 1,024, so probes for unknown ids can't evict real documents. Concurrent misses for one document share a single read. Metadata routes read only the issuer row. At most 16 distinct documents are read at once per process, each with a 2-second deadline; a miss beyond that gets a 503 with `Retry-After: 1` instead of waiting on the database pool. A cached document's `max-age` is reduced by its time in the cache, so a verifier never holds a key longer than 60 seconds after this process read it, and the activation and overlap timings still hold.

So repeated requests for the same document reach the database about once per process per 10 seconds. Requests spread over many distinct organization ids still read once per id. Volumetric abuse is left to edge rate limiting on `/oauth/idp/org/*` and the matching `/.well-known/*` paths. Add an edge rule there if traffic grows.

## IAM assumptions

- The provisioning identity can create keys on the identity provider key ring and set their IAM policy.
- The platform signing service account (`GRAM_IDENTITY_PROVIDER_SIGNING_SERVICE_ACCOUNT`) holds `roles/cloudkms.signerVerifier` on each issuer key, granted per key at creation and removed at retirement. It shares the ring with Okta connection keys; the `emaissuer-` prefix is the only separation.
