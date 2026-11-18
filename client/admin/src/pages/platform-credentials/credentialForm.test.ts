// @vitest-environment node
import { describe, expect, it } from "vitest";

import {
  EMPTY_FORM,
  authMethodOf,
  formProblem,
  requestFromForm,
  type CredentialFormState,
} from "./credentialForm";

function form(overrides: Partial<CredentialFormState>): CredentialFormState {
  return { ...EMPTY_FORM, name: "Signer", ...overrides };
}

describe("authMethodOf", () => {
  it("reads any wif field as Workload Identity Federation", () => {
    expect(
      authMethodOf({ wifPoolId: "pool", impersonateServiceAccount: "sa" }),
    ).toBe("wif");
  });

  it("reads a lone service account as impersonation", () => {
    expect(authMethodOf({ impersonateServiceAccount: "sa" })).toBe(
      "impersonation",
    );
  });

  it("reads no fields as the attached identity", () => {
    expect(authMethodOf({})).toBe("ambient");
  });
});

describe("requestFromForm", () => {
  it("drops every field the attached identity does not use", () => {
    expect(
      requestFromForm(
        form({
          method: "ambient",
          impersonateServiceAccount: "left@example.test",
          wifPoolId: "left",
        }),
      ),
    ).toEqual({ name: "Signer" });
  });

  it("drops wif fields for impersonation and trims values", () => {
    expect(
      requestFromForm(
        form({
          method: "impersonation",
          name: "  Signer ",
          impersonateServiceAccount: " signer@example.test ",
          wifPoolId: "left",
        }),
      ),
    ).toEqual({
      name: "Signer",
      impersonateServiceAccount: "signer@example.test",
    });
  });

  it("omits an empty optional hop for wif", () => {
    expect(
      requestFromForm(
        form({
          method: "wif",
          wifPoolId: "pool",
          wifProviderId: "provider",
          wifProjectNumber: "123",
        }),
      ),
    ).toEqual({
      name: "Signer",
      impersonateServiceAccount: undefined,
      wifPoolId: "pool",
      wifProviderId: "provider",
      wifProjectNumber: "123",
    });
  });
});

describe("formProblem", () => {
  it("requires a name", () => {
    expect(formProblem(form({ name: " ", method: "ambient" }))).toBe(
      "Enter a name.",
    );
  });

  it("requires the service account for impersonation", () => {
    expect(formProblem(form({ method: "impersonation" }))).toBeDefined();
  });

  it("requires the whole wif triple", () => {
    expect(
      formProblem(form({ method: "wif", wifPoolId: "p", wifProviderId: "v" })),
    ).toBeDefined();
  });

  it("requires a numeric wif project number", () => {
    expect(
      formProblem(
        form({
          method: "wif",
          wifPoolId: "p",
          wifProviderId: "v",
          wifProjectNumber: "abc",
        }),
      ),
    ).toBe("The GCP project number is digits only.");
  });

  it("accepts a complete form", () => {
    expect(
      formProblem(
        form({ method: "impersonation", impersonateServiceAccount: "sa" }),
      ),
    ).toBeUndefined();
  });
});
