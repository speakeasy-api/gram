// Reads and writes for platform external credentials: the identities Speakeasy
// authenticates into a cloud provider with on behalf of every organization.
//
// Every read and invalidation for this section goes through here, so a cache
// key is written once.

import {
  queryOptions,
  type Query,
  type QueryClient,
} from "@tanstack/react-query";
import type { GcpIamCredential } from "@gram/admin-client/models/components/gcpiamcredential";
import type { VerifyPlatformCredentialResult } from "@gram/admin-client/models/components/verifyplatformcredentialresult";
import type { CreatePlatformGcpIamCredentialForm } from "@gram/admin-client/models/components/createplatformgcpiamcredentialform";
import type { UpdateGcpIamPlatformCredentialRequestBody } from "@gram/admin-client/models/components/updategcpiamplatformcredentialrequestbody";
import type { AdminListPlatformExternalCredentialsRequest } from "@gram/admin-client/models/operations/adminlistplatformexternalcredentials";
import { buildAdminListPlatformExternalCredentialsQuery } from "@gram/admin-client/react-query/adminListPlatformExternalCredentials.core";
import { buildAdminGetGcpIamPlatformCredentialQuery } from "@gram/admin-client/react-query/adminGetGcpIamPlatformCredential.core";
import { buildAdminCreateGcpIamPlatformCredentialMutation } from "@gram/admin-client/react-query/adminCreateGcpIamPlatformCredential";
import { buildAdminUpdateGcpIamPlatformCredentialMutation } from "@gram/admin-client/react-query/adminUpdateGcpIamPlatformCredential";
import { buildAdminVerifyGcpIamPlatformCredentialMutation } from "@gram/admin-client/react-query/adminVerifyGcpIamPlatformCredential";
import { buildAdminDeleteGcpIamPlatformCredentialMutation } from "@gram/admin-client/react-query/adminDeleteGcpIamPlatformCredential";

import { redirecting, redirectingClient } from "@/lib/adminSdkClient";

function createListQuery(request: AdminListPlatformExternalCredentialsRequest) {
  const generated = buildAdminListPlatformExternalCredentialsQuery(
    redirectingClient(),
    request,
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

export function platformCredentialsListQuery(
  request: AdminListPlatformExternalCredentialsRequest = {},
): ReturnType<typeof createListQuery> {
  return createListQuery(request);
}

function createGetQuery(id: string) {
  const generated = buildAdminGetGcpIamPlatformCredentialQuery(
    redirectingClient(),
    { id },
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

export function gcpIamPlatformCredentialQuery(
  id: string,
): ReturnType<typeof createGetQuery> {
  return createGetQuery(id);
}

export function createGcpIamPlatformCredential(
  request: CreatePlatformGcpIamCredentialForm,
): Promise<GcpIamCredential> {
  return redirecting(
    buildAdminCreateGcpIamPlatformCredentialMutation(
      redirectingClient(),
    ).mutationFn({ request }),
  );
}

export function updateGcpIamPlatformCredential(
  request: UpdateGcpIamPlatformCredentialRequestBody,
): Promise<GcpIamCredential> {
  return redirecting(
    buildAdminUpdateGcpIamPlatformCredentialMutation(
      redirectingClient(),
    ).mutationFn({ request }),
  );
}

export function verifyGcpIamPlatformCredential(
  id: string,
): Promise<VerifyPlatformCredentialResult> {
  return redirecting(
    buildAdminVerifyGcpIamPlatformCredentialMutation(
      redirectingClient(),
    ).mutationFn({ request: { id } }),
  );
}

export function deleteGcpIamPlatformCredential(id: string): Promise<void> {
  return redirecting(
    buildAdminDeleteGcpIamPlatformCredentialMutation(
      redirectingClient(),
    ).mutationFn({ request: { id } }),
  );
}

// Generated keys are ["@gram/admin-client", "admin", <operation>, ...].
function isPlatformCredentialQuery(query: Query): boolean {
  const operation = query.queryKey[2];
  return (
    query.queryKey[0] === "@gram/admin-client" &&
    query.queryKey[1] === "admin" &&
    typeof operation === "string" &&
    (operation === "listPlatformExternalCredentials" ||
      operation === "getGcpIamPlatformCredential")
  );
}

// Refreshes every platform credential read. A deleted credential's detail is
// evicted first: its page can still be mounted while the operator navigates
// away, and a refetch would only report not found.
export function invalidatePlatformCredentialQueries(
  cache: QueryClient,
  deletedId?: string,
): Promise<void> {
  if (deletedId !== undefined) {
    cache.removeQueries({
      queryKey: gcpIamPlatformCredentialQuery(deletedId).queryKey,
    });
  }
  return cache.invalidateQueries({ predicate: isPlatformCredentialQuery });
}
