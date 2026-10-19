/** How the operator prefers to make the grants in their own cloud account. */
export type SetupMethod = "gcloud" | "terraform" | "console";

export type SetupValues = {
  projectId: string;
  /** The short name of the service account Speakeasy impersonates. */
  accountId: string;
  /**
   * The full email of the service account, when it is already known (a
   * connected one may live in another project or have another name).
   * Overrides the one derived from the project and account name.
   */
  serviceAccountEmail?: string;
  /** The principal Speakeasy itself runs as, granted the right to impersonate. */
  speakeasyPrincipal: string;
  location: string;
  keyRing: string;
  keyName: string;
};

/** The JWS algorithm the Cloud KMS key signs with. */
export type SigningAlgorithm = "ES256" | "RS256";

// How each algorithm is spelled by gcloud, Terraform and the console.
const KMS_ALGORITHMS: Record<
  SigningAlgorithm,
  { gcloud: string; terraform: string; console: string }
> = {
  ES256: {
    gcloud: "ec-sign-p256-sha256",
    terraform: "EC_SIGN_P256_SHA256",
    console: "Elliptic Curve P-256 – SHA256",
  },
  RS256: {
    gcloud: "rsa-sign-pkcs1-2048-sha256",
    terraform: "RSA_SIGN_PKCS1_2048_SHA256",
    console: "RSA 2048 – PKCS#1 v1.5 – SHA256",
  },
};

/** Either code to run, or console steps to follow. */
export type SetupInstructions =
  | { kind: "code"; language: string; code: string }
  | { kind: "steps"; steps: string[] };

const IMPERSONATION_ROLE = "roles/iam.serviceAccountTokenCreator";
const SIGNING_ROLE = "roles/cloudkms.signerVerifier";

// Typed values fill the snippets; blank ones stay as obvious placeholders so a
// copied command never silently runs against an empty string.
function or(value: string, placeholder: string): string {
  return value.trim() || placeholder;
}

export function serviceAccountEmail(values: SetupValues): string {
  const known = values.serviceAccountEmail?.trim();
  if (known) return known;
  return `${or(values.accountId, "SERVICE_ACCOUNT")}@${or(values.projectId, "PROJECT_ID")}.iam.gserviceaccount.com`;
}

// The stand-ins a blank field leaves in the key version name.
const RESOURCE_PLACEHOLDERS = {
  projectId: "PROJECT_ID",
  location: "LOCATION",
  keyRing: "KEY_RING",
  keyName: "KEY",
} as const;

export function keyVersionResourceName(values: SetupValues): string {
  const field = (name: keyof typeof RESOURCE_PLACEHOLDERS) =>
    or(values[name], RESOURCE_PLACEHOLDERS[name]);
  return `projects/${field("projectId")}/locations/${field("location")}/keyRings/${field("keyRing")}/cryptoKeys/${field("keyName")}/cryptoKeyVersions/1`;
}

/** A field the generated key version name needs is still blank. */
export function hasBlankResourceField(values: SetupValues): boolean {
  return (
    Object.keys(RESOURCE_PLACEHOLDERS) as (keyof typeof RESOURCE_PLACEHOLDERS)[]
  ).some((name) => !values[name].trim());
}

const KEY_VERSION_NAME =
  /^projects\/[^/\s]+\/locations\/[^/\s]+\/keyRings\/[^/\s]+\/cryptoKeys\/[^/\s]+\/cryptoKeyVersions\/\d+$/;

/** A hand-written name has the shape of a Cloud KMS key version. */
export function isKeyVersionResourceName(resourceName: string): boolean {
  return KEY_VERSION_NAME.test(resourceName.trim());
}

/** Step: create the service account and let Speakeasy impersonate it. */
export function impersonationInstructions(
  method: SetupMethod,
  values: SetupValues,
): SetupInstructions {
  const project = or(values.projectId, "PROJECT_ID");
  const account = or(values.accountId, "SERVICE_ACCOUNT");
  const principal = or(values.speakeasyPrincipal, "SPEAKEASY_PRINCIPAL");
  // The account this step creates, so the grant always targets it; an email
  // override names an account that already exists and only feeds later steps.
  const email = serviceAccountEmail({
    ...values,
    serviceAccountEmail: undefined,
  });

  switch (method) {
    case "gcloud":
      return {
        kind: "code",
        language: "sh",
        code: [
          `gcloud iam service-accounts create ${account} \\`,
          `  --project=${project} \\`,
          `  --display-name="Speakeasy signer"`,
          ``,
          `gcloud iam service-accounts add-iam-policy-binding ${email} \\`,
          `  --project=${project} \\`,
          `  --member="serviceAccount:${principal}" \\`,
          `  --role="${IMPERSONATION_ROLE}"`,
        ].join("\n"),
      };
    case "terraform":
      return {
        kind: "code",
        language: "hcl",
        code: [
          `resource "google_service_account" "speakeasy_signer" {`,
          `  project      = "${project}"`,
          `  account_id   = "${account}"`,
          `  display_name = "Speakeasy signer"`,
          `}`,
          ``,
          `resource "google_service_account_iam_member" "speakeasy_impersonation" {`,
          `  service_account_id = google_service_account.speakeasy_signer.name`,
          `  role               = "${IMPERSONATION_ROLE}"`,
          `  member             = "serviceAccount:${principal}"`,
          `}`,
        ].join("\n"),
      };
    case "console":
      return {
        kind: "steps",
        steps: [
          `In the Google Cloud console, open IAM & Admin › Service Accounts for project ${project}.`,
          `Create a service account named ${account}. Skip the optional role grants.`,
          `Open the new account, go to Principals with access, and choose Grant access.`,
          `Add ${principal} as the principal with the Service Account Token Creator role, then save.`,
        ],
      };
  }
}

/** Step: let the service account sign with keys in the key ring. */
export function keyRingInstructions(
  method: SetupMethod,
  values: SetupValues,
  algorithm: SigningAlgorithm,
): SetupInstructions {
  const project = or(values.projectId, "PROJECT_ID");
  const location = or(values.location, "LOCATION");
  const keyRing = or(values.keyRing, "KEY_RING");
  const key = or(values.keyName, "KEY");
  const email = serviceAccountEmail(values);
  const kmsAlgorithm = KMS_ALGORITHMS[algorithm];

  switch (method) {
    case "gcloud":
      return {
        kind: "code",
        language: "sh",
        code: [
          `# Skip these two if the key ring and key already exist.`,
          `gcloud kms keyrings create ${keyRing} \\`,
          `  --location=${location} --project=${project}`,
          ``,
          `gcloud kms keys create ${key} \\`,
          `  --keyring=${keyRing} --location=${location} --project=${project} \\`,
          `  --purpose=asymmetric-signing \\`,
          `  --default-algorithm=${kmsAlgorithm.gcloud}`,
          ``,
          `gcloud kms keyrings add-iam-policy-binding ${keyRing} \\`,
          `  --location=${location} --project=${project} \\`,
          `  --member="serviceAccount:${email}" \\`,
          `  --role="${SIGNING_ROLE}"`,
        ].join("\n"),
      };
    case "terraform":
      return {
        kind: "code",
        language: "hcl",
        code: [
          `resource "google_kms_key_ring" "speakeasy" {`,
          `  project  = "${project}"`,
          `  name     = "${keyRing}"`,
          `  location = "${location}"`,
          `}`,
          ``,
          `resource "google_kms_crypto_key" "speakeasy_signing" {`,
          `  name     = "${key}"`,
          `  key_ring = google_kms_key_ring.speakeasy.id`,
          `  purpose  = "ASYMMETRIC_SIGN"`,
          ``,
          `  version_template {`,
          `    algorithm = "${kmsAlgorithm.terraform}"`,
          `  }`,
          `}`,
          ``,
          `resource "google_kms_key_ring_iam_member" "speakeasy_signer" {`,
          `  key_ring_id = google_kms_key_ring.speakeasy.id`,
          `  role        = "${SIGNING_ROLE}"`,
          `  member      = "serviceAccount:${email}"`,
          `}`,
        ].join("\n"),
      };
    case "console":
      return {
        kind: "steps",
        steps: [
          `In the Google Cloud console, open Security › Key Management for project ${project}.`,
          `Use key ring ${keyRing} in ${location}, or create it.`,
          `In it, use or create key ${key} with purpose Asymmetric sign and algorithm ${kmsAlgorithm.console}.`,
          `On the key ring, choose Permissions › Grant access, add ${email}, and give it the Cloud KMS CryptoKey Signer/Verifier role.`,
        ],
      };
  }
}
