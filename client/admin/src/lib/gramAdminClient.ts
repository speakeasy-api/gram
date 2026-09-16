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
import { buildAdminDisableOrganizationMutation } from "@gram/admin-client/react-query/adminDisableOrganization";
import { buildAdminEnableOrganizationMutation } from "@gram/admin-client/react-query/adminEnableOrganization";
import { buildAdminExtendTrialMutation } from "@gram/admin-client/react-query/adminExtendTrial";
import { buildAdminRearmTrialMutation } from "@gram/admin-client/react-query/adminRearmTrial";
import { buildAdminStartTrialMutation } from "@gram/admin-client/react-query/adminStartTrial";
import type { AdminOrganization as SdkAdminOrganization } from "@gram/admin-client/models/components/adminorganization";
import type { DisableOrganizationRequestBody } from "@gram/admin-client/models/components/disableorganizationrequestbody";
import type { EnableOrganizationRequestBody } from "@gram/admin-client/models/components/enableorganizationrequestbody";
import type { ExtendTrialRequestBody } from "@gram/admin-client/models/components/extendtrialrequestbody";
import type { RearmTrialRequestBody } from "@gram/admin-client/models/components/rearmtrialrequestbody";
import type { StartTrialRequestBody } from "@gram/admin-client/models/components/starttrialrequestbody";
import type { AdminOrganization } from "@/lib/gramAdminApi";
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
import { buildAdminOrganizationGuidedReadinessQuery } from "@gram/admin-client/react-query/adminOrganizationGuidedReadiness.core";
import { buildAdminGetOrganizationDirectoryHandoffQuery } from "@gram/admin-client/react-query/adminGetOrganizationDirectoryHandoff.core";
import { buildAdminSetOrganizationDirectoryHandoffMutation } from "@gram/admin-client/react-query/adminSetOrganizationDirectoryHandoff";
import { buildAdminClearOrganizationDirectoryHandoffMutation } from "@gram/admin-client/react-query/adminClearOrganizationDirectoryHandoff";
import type { DirectoryHandoff } from "@gram/admin-client/models/components/directoryhandoff";
import type { SetOrganizationDirectoryHandoffRequestBody } from "@gram/admin-client/models/components/setorganizationdirectoryhandoffrequestbody";
import type { ClearOrganizationDirectoryHandoffRequestBody } from "@gram/admin-client/models/components/clearorganizationdirectoryhandoffrequestbody";
import { buildSetAdminOrganizationFeatureMutation } from "@gram/admin-client/react-query/setAdminOrganizationFeature";
import type { ProductFeatures } from "@gram/admin-client/models/components/productfeatures";
import type { SetOrganizationFeatureRequestBody } from "@gram/admin-client/models/components/setorganizationfeaturerequestbody";
import type { AdminGetOrganizationFeaturesRequest } from "@gram/admin-client/models/operations/admingetorganizationfeatures";
import type { AdminGetOrganizationGuidedReadinessRequest } from "@gram/admin-client/models/operations/admingetorganizationguidedreadiness";

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

// Readiness reports why the guided identity provider flow is or is not offered
// to an organization. It reads; there is nothing here to write.
function createOrganizationGuidedReadinessQuery(organizationId: string) {
  const request: AdminGetOrganizationGuidedReadinessRequest = {
    organizationId,
  };
  const generated = buildAdminOrganizationGuidedReadinessQuery(
    redirectingClient,
    request,
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

export function organizationGuidedReadinessQuery(
  organizationId: string,
): ReturnType<typeof createOrganizationGuidedReadinessQuery> {
  return createOrganizationGuidedReadinessQuery(organizationId);
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

function createOrganizationDirectoryHandoffQuery(organizationId: string) {
  const generated = buildAdminGetOrganizationDirectoryHandoffQuery(
    redirectingClient,
    { organizationId },
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    // Like the other organization-scoped reads whose endpoint can answer 404 or
    // 503: an operator waiting through three retries to be told the same thing
    // learns nothing from the wait.
    retry: false,
  });
}

export function organizationDirectoryHandoffQuery(
  organizationId: string,
): ReturnType<typeof createOrganizationDirectoryHandoffQuery> {
  return createOrganizationDirectoryHandoffQuery(organizationId);
}

// The handoff writes report in place, the way the feature write does: a 401
// taken as a redirect would sign the operator back in behind the token they
// just pasted, and the token is not in the form any more to paste again.
const setDirectoryHandoffMutation =
  buildAdminSetOrganizationDirectoryHandoffMutation(mutationClient);
const clearDirectoryHandoffMutation =
  buildAdminClearOrganizationDirectoryHandoffMutation(mutationClient);

export function setOrganizationDirectoryHandoff(
  request: SetOrganizationDirectoryHandoffRequestBody,
): Promise<DirectoryHandoff> {
  return setDirectoryHandoffMutation.mutationFn({ request });
}

export function clearOrganizationDirectoryHandoff(
  request: ClearOrganizationDirectoryHandoffRequestBody,
): Promise<void> {
  return clearDirectoryHandoffMutation.mutationFn({ request });
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

// The organization list, peek and overview all read the hand-written
// snake_case record with ISO-string dates, so every write that answers with
// the organization in its new state is reduced to that shape before it reaches
// the cache. Dates come back from the generated model as Date instances and go
// out as ISO strings; a field the server left out stays absent.
export function organizationFromSdk(
  org: SdkAdminOrganization,
): AdminOrganization {
  return {
    id: org.id,
    name: org.name,
    slug: org.slug,
    account_type: org.accountType,
    workos_id: org.workosId,
    stripe_customer_id: org.stripeCustomerId,
    stripe_subscription_id: org.stripeSubscriptionId,
    whitelisted: org.whitelisted,
    disabled_at: org.disabledAt?.toISOString(),
    trial_state: org.trialState,
    trial_ends_at: org.trialEndsAt?.toISOString(),
    trial_tier: org.trialTier,
    trial_converted_at: org.trialConvertedAt?.toISOString(),
    trial_demoted_at: org.trialDemotedAt?.toISOString(),
    member_count: org.memberCount,
    created_at: org.createdAt.toISOString(),
    updated_at: org.updatedAt.toISOString(),
  };
}

// The organization lifecycle and trial writes below all answer with the record
// in its new state, so a caller updates its cache from the response rather
// than reading the record back. Disable, enable, extend and re-arm take the
// login redirect on a 401 like every other read and write of the record.
const disableOrganizationMutation =
  buildAdminDisableOrganizationMutation(redirectingClient);
const enableOrganizationMutation =
  buildAdminEnableOrganizationMutation(redirectingClient);
const extendTrialMutation = buildAdminExtendTrialMutation(redirectingClient);
const rearmTrialMutation = buildAdminRearmTrialMutation(redirectingClient);

export async function disableOrganization(
  request: DisableOrganizationRequestBody,
): Promise<AdminOrganization> {
  return organizationFromSdk(
    await redirecting(disableOrganizationMutation.mutationFn({ request })),
  );
}

export async function enableOrganization(
  request: EnableOrganizationRequestBody,
): Promise<AdminOrganization> {
  return organizationFromSdk(
    await redirecting(enableOrganizationMutation.mutationFn({ request })),
  );
}

// The days are added to the trial's current end date, not to today, so an
// extension applied early does not shorten the trial.
export async function extendTrial(
  request: ExtendTrialRequestBody,
): Promise<AdminOrganization> {
  return organizationFromSdk(
    await redirecting(extendTrialMutation.mutationFn({ request })),
  );
}

// Not an extension with a different verb. The days are the whole length of a
// fresh run counted from now, and the write also restores the organization's
// account type and whitelist flag and revives its model provider keys. Only a
// demoted trial can be re-armed; anything else is refused with a conflict.
export async function rearmTrial(
  request: RearmTrialRequestBody,
): Promise<AdminOrganization> {
  return organizationFromSdk(
    await redirecting(rearmTrialMutation.mutationFn({ request })),
  );
}

// Grants a new enterprise trial counted from now. Only an organization that
// has never trialled, or whose trial has expired without converting or being
// demoted, can be started; anything else is refused with a conflict. A start
// reports its own 401 in place rather than taking the login redirect, which
// would sign the operator back in behind the action they just took.
const startTrialMutation = buildAdminStartTrialMutation(mutationClient);

export async function startTrial(
  request: StartTrialRequestBody,
): Promise<AdminOrganization> {
  return organizationFromSdk(await startTrialMutation.mutationFn({ request }));
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
