// The server infers how a platform GCP credential authenticates from which
// fields are set. The editor makes that choice explicit, so an operator cannot
// leave a stray field behind that silently changes the method.

import type { GcpIamCredential } from "@gram/admin-client/models/components/gcpiamcredential";
import type { CreatePlatformGcpIamCredentialForm } from "@gram/admin-client/models/components/createplatformgcpiamcredentialform";

export type GcpAuthMethod = "ambient" | "impersonation" | "wif";

export const GCP_AUTH_METHODS: readonly {
  value: GcpAuthMethod;
  label: string;
  description: string;
}[] = [
  {
    value: "ambient",
    label: "Attached identity",
    description:
      "Speakeasy's own workload identity, with no impersonation hop.",
  },
  {
    value: "impersonation",
    label: "Service account impersonation",
    description:
      "Speakeasy's identity impersonates the named service account directly.",
  },
  {
    value: "wif",
    label: "Workload Identity Federation",
    description:
      "Speakeasy exchanges a federated token through a WIF pool, optionally impersonating a service account after.",
  },
];

export type CredentialFormState = {
  name: string;
  method: GcpAuthMethod;
  impersonateServiceAccount: string;
  wifPoolId: string;
  wifProviderId: string;
  wifProjectNumber: string;
};

export const EMPTY_FORM: CredentialFormState = {
  name: "",
  method: "impersonation",
  impersonateServiceAccount: "",
  wifPoolId: "",
  wifProviderId: "",
  wifProjectNumber: "",
};

export function authMethodOf(
  credential: Pick<
    GcpIamCredential,
    | "impersonateServiceAccount"
    | "wifPoolId"
    | "wifProviderId"
    | "wifProjectNumber"
  >,
): GcpAuthMethod {
  if (
    credential.wifPoolId ||
    credential.wifProviderId ||
    credential.wifProjectNumber
  ) {
    return "wif";
  }
  if (credential.impersonateServiceAccount) return "impersonation";
  return "ambient";
}

export function authMethodLabel(method: GcpAuthMethod): string {
  return GCP_AUTH_METHODS.find((m) => m.value === method)?.label ?? method;
}

export function formFromCredential(
  credential: GcpIamCredential,
): CredentialFormState {
  return {
    name: credential.name,
    method: authMethodOf(credential),
    impersonateServiceAccount: credential.impersonateServiceAccount ?? "",
    wifPoolId: credential.wifPoolId ?? "",
    wifProviderId: credential.wifProviderId ?? "",
    wifProjectNumber: credential.wifProjectNumber ?? "",
  };
}

function present(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed === "" ? undefined : trimmed;
}

// Why the form cannot be submitted yet, or undefined when it can. The server
// validates the same rules; checking here keeps the error beside the field.
export function formProblem(form: CredentialFormState): string | undefined {
  if (!present(form.name)) return "Enter a name.";
  if (
    form.method === "impersonation" &&
    !present(form.impersonateServiceAccount)
  ) {
    return "Enter the service account to impersonate.";
  }
  if (
    form.method === "wif" &&
    (!present(form.wifPoolId) ||
      !present(form.wifProviderId) ||
      !present(form.wifProjectNumber))
  ) {
    return "Enter the pool ID, provider ID and project number.";
  }
  if (form.method === "wif" && !/^\d+$/.test(form.wifProjectNumber.trim())) {
    return "The GCP project number is digits only.";
  }
  return undefined;
}

// Sends only the fields the chosen method uses. Update is a full replace, so a
// field left out is cleared on the stored credential.
export function requestFromForm(
  form: CredentialFormState,
): CreatePlatformGcpIamCredentialForm {
  const name = form.name.trim();
  switch (form.method) {
    case "ambient":
      return { name };
    case "impersonation":
      return {
        name,
        impersonateServiceAccount: present(form.impersonateServiceAccount),
      };
    case "wif":
      return {
        name,
        impersonateServiceAccount: present(form.impersonateServiceAccount),
        wifPoolId: present(form.wifPoolId),
        wifProviderId: present(form.wifProviderId),
        wifProjectNumber: present(form.wifProjectNumber),
      };
  }
}
