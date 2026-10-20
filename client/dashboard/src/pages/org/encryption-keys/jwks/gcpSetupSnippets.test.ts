import { describe, expect, it } from "vitest";
import {
  hasBlankResourceField,
  impersonationInstructions,
  isKeyVersionResourceName,
  keyRingInstructions,
  keyVersionResourceName,
  type SetupValues,
} from "./gcpSetupSnippets";

const values: SetupValues = {
  projectId: "acme-prod",
  accountId: "speakeasy-signer",
  speakeasyPrincipal: "speakeasy@example.iam.gserviceaccount.com",
  location: "global",
  keyRing: "speakeasy",
  keyName: "speakeasy-signing",
};

function text(instructions: ReturnType<typeof keyRingInstructions>): string {
  return instructions.kind === "code"
    ? instructions.code
    : instructions.steps.join("\n");
}

describe("keyRingInstructions", () => {
  it("grants a known service account email instead of the derived one", () => {
    const known = {
      ...values,
      serviceAccountEmail: "signer@other-project.iam.gserviceaccount.com",
    };
    for (const method of ["gcloud", "terraform", "console"] as const) {
      const out = text(keyRingInstructions(method, known, "ES256"));
      expect(out).toContain("signer@other-project.iam.gserviceaccount.com");
      expect(out).not.toContain("speakeasy-signer@acme-prod");
      // The key ring still lives in the typed project.
      expect(out).toContain("acme-prod");
    }
  });

  it("derives the email when none is known", () => {
    expect(
      text(
        keyRingInstructions(
          "gcloud",
          { ...values, serviceAccountEmail: " " },
          "ES256",
        ),
      ),
    ).toContain(
      "serviceAccount:speakeasy-signer@acme-prod.iam.gserviceaccount.com",
    );
  });

  it("creates an EC key for ES256", () => {
    expect(text(keyRingInstructions("gcloud", values, "ES256"))).toContain(
      "--default-algorithm=ec-sign-p256-sha256",
    );
    expect(text(keyRingInstructions("terraform", values, "ES256"))).toContain(
      'algorithm = "EC_SIGN_P256_SHA256"',
    );
    expect(text(keyRingInstructions("console", values, "ES256"))).toContain(
      "Elliptic Curve P-256",
    );
  });

  it("creates an RSA key for RS256", () => {
    const gcloud = text(keyRingInstructions("gcloud", values, "RS256"));
    expect(gcloud).toContain("--default-algorithm=rsa-sign-pkcs1-2048-sha256");
    expect(gcloud).not.toContain("ec-sign");
    expect(text(keyRingInstructions("terraform", values, "RS256"))).toContain(
      'algorithm = "RSA_SIGN_PKCS1_2048_SHA256"',
    );
    const console = text(keyRingInstructions("console", values, "RS256"));
    expect(console).toContain("RSA 2048");
    expect(console).not.toContain("Elliptic Curve");
  });
});

describe("impersonationInstructions", () => {
  it("grants on the account it creates, whatever email later steps use", () => {
    const edited = {
      ...values,
      serviceAccountEmail: "signer@other-project.iam.gserviceaccount.com",
    };
    for (const method of ["gcloud", "terraform", "console"] as const) {
      const out = text(impersonationInstructions(method, edited));
      expect(out).not.toContain("signer@other-project");
    }
    // Each method grants on the account it creates.
    expect(text(impersonationInstructions("gcloud", edited))).toContain(
      "add-iam-policy-binding speakeasy-signer@acme-prod.iam.gserviceaccount.com",
    );
    expect(text(impersonationInstructions("terraform", edited))).toContain(
      "service_account_id = google_service_account.speakeasy_signer.name",
    );
    const console = text(impersonationInstructions("console", edited));
    expect(console).toContain(
      "Create a service account named speakeasy-signer",
    );
    expect(console).toContain("Open the new account");
  });
});

describe("hasBlankResourceField", () => {
  it("flags any field the key version name needs left blank", () => {
    expect(hasBlankResourceField(values)).toBe(false);
    for (const field of ["projectId", "location", "keyRing", "keyName"]) {
      expect(hasBlankResourceField({ ...values, [field]: " " })).toBe(true);
    }
  });

  it("allows real names spelled like a placeholder", () => {
    expect(
      hasBlankResourceField({
        ...values,
        projectId: "PROJECT_ID",
        location: "LOCATION",
        keyRing: "KEY_RING",
        keyName: "KEY",
      }),
    ).toBe(false);
  });
});

describe("isKeyVersionResourceName", () => {
  it("accepts a full key version name", () => {
    expect(isKeyVersionResourceName(keyVersionResourceName(values))).toBe(true);
    expect(
      isKeyVersionResourceName(
        "  projects/p/locations/global/keyRings/ring/cryptoKeys/KEY/cryptoKeyVersions/12  ",
      ),
    ).toBe(true);
  });

  it("rejects names with a missing, empty or non-numeric part", () => {
    for (const name of [
      "",
      "projects/p/locations/global/keyRings/ring/cryptoKeys/key",
      "projects/p/locations/global/keyRings/ring/cryptoKeys/key/cryptoKeyVersions/",
      "projects/p/locations/global/keyRings/ring/cryptoKeys/key/cryptoKeyVersions/latest",
      "projects//locations/global/keyRings/ring/cryptoKeys/key/cryptoKeyVersions/1",
      "projects/p/locations/global/keyRings/ring/cryptoKeys/key/cryptoKeyVersions/1/extra",
    ]) {
      expect(isKeyVersionResourceName(name)).toBe(false);
    }
  });
});
