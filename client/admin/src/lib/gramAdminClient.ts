import {
  queryOptions,
  useMutation,
  type UseMutationOptions,
  type UseMutationResult,
} from "@tanstack/react-query";

import { GramCore } from "@gram/admin-client/core";
import { buildAdminAdminGetSessionQuery } from "@gram/admin-client/react-query/adminAdminGetSession.core";
import { buildAdminOrganizationFeaturesQuery } from "@gram/admin-client/react-query/adminOrganizationFeatures.core";
import { buildSetAdminOrganizationFeatureMutation } from "@gram/admin-client/react-query/setAdminOrganizationFeature";
import type { ProductFeatures } from "@gram/admin-client/models/components/productfeatures";
import type { SetOrganizationFeatureRequestBody } from "@gram/admin-client/models/components/setorganizationfeaturerequestbody";
import type { AdminGetOrganizationFeaturesRequest } from "@gram/admin-client/models/operations/admingetorganizationfeatures";

// Speakeasy requires an absolute base URL. Keep the generated client private so
// callers cannot replace this origin or supply raw SDK/request options. Browser
// fetch defaults preserve the ambient first-party gram_admin cookie; do not set
// credentials, mode, Authorization, or Cookie here.
const client = new GramCore({ serverURL: window.location.origin });

let redirectingToLogin = false;

export function isRedirectingToLogin(): boolean {
  return redirectingToLogin;
}

function statusCode(error: unknown): number | undefined {
  if (!error || typeof error !== "object") return undefined;
  if ("statusCode" in error && typeof error.statusCode === "number") {
    return error.statusCode;
  }
  if ("status" in error && typeof error.status === "number") {
    return error.status;
  }
  return undefined;
}

// Shared by generated operations and the handwritten predecessor during the
// consumer migration. Assignment starts navigation but does not unwind callers,
// so the original error remains the operation result.
export function redirectOnUnauthorized(error: unknown): never {
  if (statusCode(error) === 401 && !redirectingToLogin) {
    const returnTo = encodeURIComponent(
      window.location.pathname + window.location.search,
    );
    redirectingToLogin = true;
    window.location.href = `/admin/auth.login?return_to=${returnTo}&prompt=consent`;
  }
  throw error;
}

async function redirecting<T>(operation: Promise<T>): Promise<T> {
  try {
    return await operation;
  } catch (error) {
    return redirectOnUnauthorized(error);
  }
}

function createAdminSessionQuery() {
  const generated = buildAdminAdminGetSessionQuery(client);
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    staleTime: Infinity,
  });
}

export function adminSessionQuery(): ReturnType<
  typeof createAdminSessionQuery
> {
  return createAdminSessionQuery();
}

function createOrganizationFeaturesQuery(organizationId: string) {
  const request: AdminGetOrganizationFeaturesRequest = { organizationId };
  const generated = buildAdminOrganizationFeaturesQuery(client, request);
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

export function organizationFeaturesQuery(
  organizationId: string,
): ReturnType<typeof createOrganizationFeaturesQuery> {
  return createOrganizationFeaturesQuery(organizationId);
}

// Feature writes preserve their predecessor's in-place 401 behavior. No raw
// generated RequestOptions are accepted or forwarded.
const generatedFeatureMutation =
  buildSetAdminOrganizationFeatureMutation(client);

export function setAdminOrganizationFeature(
  request: SetOrganizationFeatureRequestBody,
): Promise<ProductFeatures> {
  return generatedFeatureMutation.mutationFn({ request });
}

type SetFeatureMutationOptions = Omit<
  UseMutationOptions<ProductFeatures, Error, SetOrganizationFeatureRequestBody>,
  "mutationFn" | "mutationKey"
>;

export function useSetAdminOrganizationFeatureMutation(
  options?: SetFeatureMutationOptions,
): UseMutationResult<
  ProductFeatures,
  Error,
  SetOrganizationFeatureRequestBody
> {
  return useMutation({
    ...options,
    mutationKey: ["@gram/admin-client", "admin", "adminSetOrganizationFeature"],
    mutationFn: setAdminOrganizationFeature,
  });
}
