import {
  buildAdminUploadPlatformImageMutation,
  type AdminUploadPlatformImageMutationVariables,
} from "@gram/admin-client/react-query/adminUploadPlatformImage";
import {
  buildAdminMigrateToGlobalIssuerMutation,
  type AdminMigrateToGlobalIssuerMutationVariables,
} from "@gram/admin-client/react-query/adminMigrateToGlobalIssuer";
import {
  buildAdminRefreshGlobalIssuerMetadataMutation,
  type AdminRefreshGlobalIssuerMetadataMutationVariables,
} from "@gram/admin-client/react-query/adminRefreshGlobalIssuerMetadata";
import {
  buildAdminFetchGlobalIssuerMetadataMutation,
  type AdminFetchGlobalIssuerMetadataMutationVariables,
} from "@gram/admin-client/react-query/adminFetchGlobalIssuerMetadata";
import {
  buildAdminDeleteGlobalIssuerMutation,
  type AdminDeleteGlobalIssuerMutationVariables,
} from "@gram/admin-client/react-query/adminDeleteGlobalIssuer";
import {
  buildAdminUpdateGlobalIssuerMutation,
  type AdminUpdateGlobalIssuerMutationVariables,
} from "@gram/admin-client/react-query/adminUpdateGlobalIssuer";
import {
  buildAdminCreateGlobalIssuerMutation,
  type AdminCreateGlobalIssuerMutationVariables,
} from "@gram/admin-client/react-query/adminCreateGlobalIssuer";
import { buildAdminServeImageQuery } from "@gram/admin-client/react-query/adminServeImage.core";
import { buildAdminGetGlobalIssuerMigratePreflightQuery } from "@gram/admin-client/react-query/adminGetGlobalIssuerMigratePreflight.core";
import { buildAdminGetGlobalIssuerDuplicatePreflightQuery } from "@gram/admin-client/react-query/adminGetGlobalIssuerDuplicatePreflight.core";
import { buildAdminListGlobalIssuerConvergenceCandidatesQuery } from "@gram/admin-client/react-query/adminListGlobalIssuerConvergenceCandidates.core";
import { buildAdminListGlobalIssuersQuery } from "@gram/admin-client/react-query/adminListGlobalIssuers.core";
import { buildAdminGetGlobalIssuerQuery } from "@gram/admin-client/react-query/adminGetGlobalIssuer.core";
import {
  infiniteQueryOptions,
  queryOptions,
  useMutation,
  type UseMutationOptions,
  type UseMutationResult,
} from "@tanstack/react-query";

import { GramCore } from "@gram/admin-client/core";
import { HTTPClient } from "@gram/admin-client/lib/http";
import { buildAdminGetSessionQuery } from "@gram/admin-client/react-query/adminGetSession.core";
import {
  buildAdminListOrganizationActivityInfiniteQuery,
  type AdminListOrganizationActivityPageParams,
} from "@gram/admin-client/react-query/adminListOrganizationActivity.core";
import { buildAdminOrganizationFeaturesQuery } from "@gram/admin-client/react-query/adminOrganizationFeatures.core";
import { buildAdminOrganizationOnboardingQuery } from "@gram/admin-client/react-query/adminOrganizationOnboarding.core";
import { buildSetAdminOrganizationOnboardingMutation } from "@gram/admin-client/react-query/setAdminOrganizationOnboarding";
import type { AdminOnboardingConfiguration } from "@gram/admin-client/models/components/adminonboardingconfiguration";
import type { SetOrganizationOnboardingRequestBody } from "@gram/admin-client/models/components/setorganizationonboardingrequestbody";
import { buildSetAdminOrganizationFeatureMutation } from "@gram/admin-client/react-query/setAdminOrganizationFeature";
import type { ProductFeatures } from "@gram/admin-client/models/components/productfeatures";
import type { SetOrganizationFeatureRequestBody } from "@gram/admin-client/models/components/setorganizationfeaturerequestbody";
import type { AdminGetOrganizationFeaturesRequest } from "@gram/admin-client/models/operations/admingetorganizationfeatures";

// Speakeasy requires an absolute base URL. Keep the generated client private so
// callers cannot replace this origin or supply raw SDK/request options. Browser
// fetch defaults preserve the ambient first-party gram_admin cookie; do not set
// credentials, mode, Authorization, or Cookie here.
const mutationClient = new GramCore({ serverURL: window.location.origin });
const redirectingHTTPClient = new HTTPClient().addHook("response", (response) =>
  startLoginRedirect(response),
);
const redirectingClient = new GramCore({
  serverURL: window.location.origin,
  httpClient: redirectingHTTPClient,
});

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

function startLoginRedirect(error: unknown): void {
  if (statusCode(error) === 401 && !redirectingToLogin) {
    const returnTo = encodeURIComponent(
      window.location.pathname + window.location.search,
    );
    redirectingToLogin = true;
    window.location.href = `/admin/auth.login?return_to=${returnTo}&prompt=consent`;
  }
}

// Shared by generated operations and the handwritten predecessor during the
// consumer migration. Assignment starts navigation but does not unwind callers,
// so the original error remains the operation result.
export function redirectOnUnauthorized(error: unknown): never {
  startLoginRedirect(error);
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
  const generated = buildAdminGetSessionQuery(redirectingClient);
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
  const generated = buildAdminOrganizationFeaturesQuery(
    redirectingClient,
    request,
  );
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

function createOrganizationOnboardingQuery(organizationId: string) {
  const generated = buildAdminOrganizationOnboardingQuery(redirectingClient, {
    organizationId,
  });
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

export function organizationOnboardingQuery(
  organizationId: string,
): ReturnType<typeof createOrganizationOnboardingQuery> {
  return createOrganizationOnboardingQuery(organizationId);
}

const generatedOnboardingMutation =
  buildSetAdminOrganizationOnboardingMutation(mutationClient);

export function setAdminOrganizationOnboarding(
  request: SetOrganizationOnboardingRequestBody,
): Promise<AdminOnboardingConfiguration> {
  return generatedOnboardingMutation.mutationFn({ request });
}

function createOrganizationActivityQuery(organizationId: string) {
  const generated = buildAdminListOrganizationActivityInfiniteQuery(
    redirectingClient,
    { organizationId },
  );
  return infiniteQueryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    initialPageParam: undefined as AdminListOrganizationActivityPageParams,
    getNextPageParam: (lastPage) => lastPage["~next"],
  });
}

export function organizationActivityQuery(
  organizationId: string,
): ReturnType<typeof createOrganizationActivityQuery> {
  return createOrganizationActivityQuery(organizationId);
}

// Feature writes preserve their predecessor's in-place 401 behavior. No raw
// generated RequestOptions are accepted or forwarded.
const generatedFeatureMutation =
  buildSetAdminOrganizationFeatureMutation(mutationClient);

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

function createAdminGetGlobalIssuerQuery(
  request: Parameters<typeof buildAdminGetGlobalIssuerQuery>[1],
) {
  const generated = buildAdminGetGlobalIssuerQuery(redirectingClient, request);
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

function createAdminListGlobalIssuersQuery(
  request: Parameters<typeof buildAdminListGlobalIssuersQuery>[1],
) {
  const generated = buildAdminListGlobalIssuersQuery(
    redirectingClient,
    request,
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

function createAdminListGlobalIssuerConvergenceCandidatesQuery(
  request: Parameters<
    typeof buildAdminListGlobalIssuerConvergenceCandidatesQuery
  >[1],
) {
  const generated = buildAdminListGlobalIssuerConvergenceCandidatesQuery(
    redirectingClient,
    request,
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

function createAdminGetGlobalIssuerDuplicatePreflightQuery(
  request: Parameters<
    typeof buildAdminGetGlobalIssuerDuplicatePreflightQuery
  >[1],
) {
  const generated = buildAdminGetGlobalIssuerDuplicatePreflightQuery(
    redirectingClient,
    request,
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

function createAdminGetGlobalIssuerMigratePreflightQuery(
  request: Parameters<typeof buildAdminGetGlobalIssuerMigratePreflightQuery>[1],
) {
  const generated = buildAdminGetGlobalIssuerMigratePreflightQuery(
    redirectingClient,
    request,
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

export function adminCreateGlobalIssuer(
  request: AdminCreateGlobalIssuerMutationVariables["request"],
): ReturnType<
  ReturnType<typeof buildAdminCreateGlobalIssuerMutation>["mutationFn"]
> {
  return redirecting(
    buildAdminCreateGlobalIssuerMutation(redirectingClient).mutationFn({
      request,
    }),
  );
}

export function adminUpdateGlobalIssuer(
  request: AdminUpdateGlobalIssuerMutationVariables["request"],
): ReturnType<
  ReturnType<typeof buildAdminUpdateGlobalIssuerMutation>["mutationFn"]
> {
  return redirecting(
    buildAdminUpdateGlobalIssuerMutation(redirectingClient).mutationFn({
      request,
    }),
  );
}

export function adminDeleteGlobalIssuer(
  request: AdminDeleteGlobalIssuerMutationVariables["request"],
): ReturnType<
  ReturnType<typeof buildAdminDeleteGlobalIssuerMutation>["mutationFn"]
> {
  return redirecting(
    buildAdminDeleteGlobalIssuerMutation(redirectingClient).mutationFn({
      request,
    }),
  );
}

export function adminFetchGlobalIssuerMetadata(
  request: AdminFetchGlobalIssuerMetadataMutationVariables["request"],
): ReturnType<
  ReturnType<typeof buildAdminFetchGlobalIssuerMetadataMutation>["mutationFn"]
> {
  return redirecting(
    buildAdminFetchGlobalIssuerMetadataMutation(redirectingClient).mutationFn({
      request,
    }),
  );
}

export function adminRefreshGlobalIssuerMetadata(
  request: AdminRefreshGlobalIssuerMetadataMutationVariables["request"],
): ReturnType<
  ReturnType<typeof buildAdminRefreshGlobalIssuerMetadataMutation>["mutationFn"]
> {
  return redirecting(
    buildAdminRefreshGlobalIssuerMetadataMutation(redirectingClient).mutationFn(
      { request },
    ),
  );
}

export function adminMigrateToGlobalIssuer(
  request: AdminMigrateToGlobalIssuerMutationVariables["request"],
): ReturnType<
  ReturnType<typeof buildAdminMigrateToGlobalIssuerMutation>["mutationFn"]
> {
  return redirecting(
    buildAdminMigrateToGlobalIssuerMutation(redirectingClient).mutationFn({
      request,
    }),
  );
}

export function adminUploadPlatformImage(
  request: AdminUploadPlatformImageMutationVariables["request"],
): ReturnType<
  ReturnType<typeof buildAdminUploadPlatformImageMutation>["mutationFn"]
> {
  return redirecting(
    buildAdminUploadPlatformImageMutation(redirectingClient).mutationFn({
      request,
    }),
  );
}

function createAdminIssuerImageQuery(id: string) {
  const generated = buildAdminServeImageQuery(redirectingClient, { id });
  return queryOptions({
    queryKey: generated.queryKey,
    queryFn: async (context) => {
      const response = await redirecting(generated.queryFn(context));
      return new Response(response.result, {
        headers: {
          "Content-Type":
            response.headers["Content-Type"]?.[0] ??
            response.headers["content-type"]?.[0] ??
            "application/octet-stream",
        },
      }).blob();
    },
  });
}

export function adminGetGlobalIssuerQuery(
  request: Parameters<typeof buildAdminGetGlobalIssuerQuery>[1],
): ReturnType<typeof createAdminGetGlobalIssuerQuery> {
  return createAdminGetGlobalIssuerQuery(request);
}

export function adminListGlobalIssuersQuery(
  request: Parameters<typeof buildAdminListGlobalIssuersQuery>[1],
): ReturnType<typeof createAdminListGlobalIssuersQuery> {
  return createAdminListGlobalIssuersQuery(request);
}

export function adminListGlobalIssuerConvergenceCandidatesQuery(
  request: Parameters<
    typeof buildAdminListGlobalIssuerConvergenceCandidatesQuery
  >[1],
): ReturnType<typeof createAdminListGlobalIssuerConvergenceCandidatesQuery> {
  return createAdminListGlobalIssuerConvergenceCandidatesQuery(request);
}

export function adminGetGlobalIssuerDuplicatePreflightQuery(
  request: Parameters<
    typeof buildAdminGetGlobalIssuerDuplicatePreflightQuery
  >[1],
): ReturnType<typeof createAdminGetGlobalIssuerDuplicatePreflightQuery> {
  return createAdminGetGlobalIssuerDuplicatePreflightQuery(request);
}

export function adminGetGlobalIssuerMigratePreflightQuery(
  request: Parameters<typeof buildAdminGetGlobalIssuerMigratePreflightQuery>[1],
): ReturnType<typeof createAdminGetGlobalIssuerMigratePreflightQuery> {
  return createAdminGetGlobalIssuerMigratePreflightQuery(request);
}

export function adminIssuerImageQuery(
  id: string,
): ReturnType<typeof createAdminIssuerImageQuery> {
  return createAdminIssuerImageQuery(id);
}
