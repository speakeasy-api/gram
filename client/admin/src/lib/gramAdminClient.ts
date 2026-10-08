import type {
  AdminSetRegistryEntryPublishedMutationData,
  AdminSetRegistryEntryPublishedMutationError,
  AdminSetRegistryEntryPublishedMutationVariables,
} from "@gram/admin-client/react-query/adminSetRegistryEntryPublished";
import type {
  AdminSaveRegistryEntryMutationData,
  AdminSaveRegistryEntryMutationError,
  AdminSaveRegistryEntryMutationVariables,
} from "@gram/admin-client/react-query/adminSaveRegistryEntry";
import type {
  AdminCreateRegistryEntryMutationData,
  AdminCreateRegistryEntryMutationError,
  AdminCreateRegistryEntryMutationVariables,
} from "@gram/admin-client/react-query/adminCreateRegistryEntry";
import type { AdminListRegistryEntriesRequest } from "@gram/admin-client/models/operations/adminlistregistryentries";
import { buildAdminListRegistryEntriesQuery } from "@gram/admin-client/react-query/adminListRegistryEntries.core";
import { buildAdminGetRegistryEntryQuery } from "@gram/admin-client/react-query/adminGetRegistryEntry.core";
import { buildAdminGetRegistryOktaCandidatesQuery } from "@gram/admin-client/react-query/adminGetRegistryOktaCandidates.core";
import { buildAdminListRegistryOktaUnmappedQuery } from "@gram/admin-client/react-query/adminListRegistryOktaUnmapped.core";
import { buildAdminCreateRegistryEntryMutation } from "@gram/admin-client/react-query/adminCreateRegistryEntry";
import { buildAdminSaveRegistryEntryMutation } from "@gram/admin-client/react-query/adminSaveRegistryEntry";
import { buildAdminSetRegistryEntryPublishedMutation } from "@gram/admin-client/react-query/adminSetRegistryEntryPublished";
import type { UseQueryOptions } from "@tanstack/react-query";
import type { AdminMeterUsageResponse } from "@gram/admin-client/models/components/adminmeterusageresponse";
import { buildAdminGetMeterUsageQuery } from "@gram/admin-client/react-query/adminGetMeterUsage.core";
import type { AdminGetMeterUsageRequest } from "@gram/admin-client/models/operations/admingetmeterusage";
import type { AdminSpendBreakdownResponse } from "@gram/admin-client/models/components/adminspendbreakdownresponse";
import { buildAdminGetSpendBreakdownQuery } from "@gram/admin-client/react-query/adminGetSpendBreakdown.core";
import type { AdminGetSpendBreakdownRequest } from "@gram/admin-client/models/operations/admingetspendbreakdown";
import type { AdminCustomerUsageResponse } from "@gram/admin-client/models/components/admincustomerusageresponse";
import { buildAdminListCustomerUsageQuery } from "@gram/admin-client/react-query/adminListCustomerUsage.core";
import type { AdminListCustomerUsageRequest } from "@gram/admin-client/models/operations/adminlistcustomerusage";
import { buildAdminDescribeMcpServerHealthQuery } from "@gram/admin-client/react-query/adminDescribeMcpServerHealth.core";
import { buildAdminGetMcpServerToolCallsQuery } from "@gram/admin-client/react-query/adminGetMcpServerToolCalls.core";
import { buildAdminSetMcpServerScopePinMutation } from "@gram/admin-client/react-query/adminSetMcpServerScopePin";
import type { SetMcpServerScopePinRequestBody } from "@gram/admin-client/models/components/setmcpserverscopepinrequestbody";
import type { AdminMcpServerResourceScopes } from "@gram/admin-client/models/components/adminmcpserverresourcescopes";
import { buildAdminChangeTrialEndDateMutation } from "@gram/admin-client/react-query/adminChangeTrialEndDate";
import type { ChangeTrialEndDateRequestBody } from "@gram/admin-client/models/components/changetrialenddaterequestbody";
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
import { adminGetStripeSubscriptionCandidate } from "@gram/admin-client/funcs/adminGetStripeSubscriptionCandidate";
import { buildAdminDisableOrganizationMutation } from "@gram/admin-client/react-query/adminDisableOrganization";
import { buildAdminEnableOrganizationMutation } from "@gram/admin-client/react-query/adminEnableOrganization";
import { buildAdminExtendTrialMutation } from "@gram/admin-client/react-query/adminExtendTrial";
import { buildAdminRearmTrialMutation } from "@gram/admin-client/react-query/adminRearmTrial";
import { buildAdminSetStripeSubscriptionMutation } from "@gram/admin-client/react-query/adminSetStripeSubscription";
import { buildAdminStartTrialMutation } from "@gram/admin-client/react-query/adminStartTrial";
import type { AdminOrganization as SdkAdminOrganization } from "@gram/admin-client/models/components/adminorganization";
import type { AdminStripeSubscriptionCandidate } from "@gram/admin-client/models/components/adminstripesubscriptioncandidate";
import type { DisableOrganizationRequestBody } from "@gram/admin-client/models/components/disableorganizationrequestbody";
import type { EnableOrganizationRequestBody } from "@gram/admin-client/models/components/enableorganizationrequestbody";
import type { ExtendTrialRequestBody } from "@gram/admin-client/models/components/extendtrialrequestbody";
import type { RearmTrialRequestBody } from "@gram/admin-client/models/components/rearmtrialrequestbody";
import type { SetStripeSubscriptionRequestBody } from "@gram/admin-client/models/components/setstripesubscriptionrequestbody";
import type { StartTrialRequestBody } from "@gram/admin-client/models/components/starttrialrequestbody";
import type { AdminGetStripeSubscriptionCandidateRequest } from "@gram/admin-client/models/operations/admingetstripesubscriptioncandidate";
import { unwrapAsync } from "@gram/admin-client/types/fp";
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
import { buildAdminOnboardingStackOptionsQuery } from "@gram/admin-client/react-query/adminOnboardingStackOptions.core";
import { buildAdminOrganizationOnboardingStackQuery } from "@gram/admin-client/react-query/adminOrganizationOnboardingStack.core";
import { buildAdminOnboardingStepsQuery } from "@gram/admin-client/react-query/adminOnboardingSteps.core";
import { buildSetAdminOrganizationOnboardingStackMutation } from "@gram/admin-client/react-query/setAdminOrganizationOnboardingStack";
import type { AdminOnboardingStack } from "@gram/admin-client/models/components/adminonboardingstack";
import type { SetOrganizationOnboardingStackRequestBody } from "@gram/admin-client/models/components/setorganizationonboardingstackrequestbody";
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

export function organizationMeterUsageQuery(
  request: AdminGetMeterUsageRequest,
): UseQueryOptions<AdminMeterUsageResponse> {
  const generated = buildAdminGetMeterUsageQuery(redirectingClient, request);
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    staleTime: 30_000,
  });
}
export function customerUsageQuery(
  request: AdminListCustomerUsageRequest,
): UseQueryOptions<AdminCustomerUsageResponse> {
  const generated = buildAdminListCustomerUsageQuery(
    redirectingClient,
    request,
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    staleTime: 30_000,
  });
}
export function organizationSpendBreakdownQuery(
  request: AdminGetSpendBreakdownRequest,
): UseQueryOptions<AdminSpendBreakdownResponse> {
  const generated = buildAdminGetSpendBreakdownQuery(
    redirectingClient,
    request,
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    staleTime: 30_000,
  });
}

// Keyed on the route's own address for the organization, id or slug as typed,
// the way `projectQuery` is, so the name entry the breadcrumb watches can be
// built from the route alone. A missing window is the default one.
function mcpServerHealthKey(
  organizationIdOrSlug: string,
  projectId: string,
  mcpServerId: string,
  windowDays: 14 | 30 | 90 = 14,
): readonly [string, string, string, string, number] {
  return [
    "gram-admin-mcp-server-health",
    organizationIdOrSlug,
    projectId,
    mcpServerId,
    windowDays,
  ] as const;
}

// The server's name alone, with no window in the key, for the breadcrumb.
// Every window's health read writes it as it lands, so changing the window
// never empties the crumb while the new answer is on its way.
export function mcpServerNameKey(
  organizationIdOrSlug: string,
  projectId: string,
  mcpServerId: string,
): readonly [string, string, string, string] {
  return [
    "gram-admin-mcp-server-name",
    organizationIdOrSlug,
    projectId,
    mcpServerId,
  ] as const;
}

type McpServerHealthRequest = {
  organizationId: string;
  projectId: string;
  mcpServerId: string;
  windowDays: 14 | 30 | 90;
};

export function mcpServerHealthQuery(
  organizationIdOrSlug: string,
  request: McpServerHealthRequest,
): ReturnType<typeof createMcpServerHealthQuery> {
  return createMcpServerHealthQuery(organizationIdOrSlug, request);
}

function createMcpServerHealthQuery(
  organizationIdOrSlug: string,
  request: McpServerHealthRequest,
) {
  const generated = buildAdminDescribeMcpServerHealthQuery(
    redirectingClient,
    request,
  );
  return queryOptions({
    queryKey: mcpServerHealthKey(
      organizationIdOrSlug,
      request.projectId,
      request.mcpServerId,
      request.windowDays,
    ),
    queryFn: async (context) => {
      const health = await redirecting(generated.queryFn(context));
      context.client.setQueryData(
        mcpServerNameKey(
          organizationIdOrSlug,
          request.projectId,
          request.mcpServerId,
        ),
        { name: health.server.name },
      );
      return health;
    },
    staleTime: 30_000,
  });
}

// Every window's health read of one server: a pin write changes what each of
// them reports.
export function mcpServerHealthServerKey(
  organizationIdOrSlug: string,
  projectId: string,
  mcpServerId: string,
): readonly [string, string, string, string] {
  return [
    "gram-admin-mcp-server-health",
    organizationIdOrSlug,
    projectId,
    mcpServerId,
  ] as const;
}

export function adminSetMcpServerScopePin(
  request: SetMcpServerScopePinRequestBody,
): Promise<AdminMcpServerResourceScopes> {
  return redirecting(
    buildAdminSetMcpServerScopePinMutation(redirectingClient).mutationFn({
      request,
    }),
  );
}

// Tool call telemetry for the same server, keyed the same way so the two
// queries for one page share every part of their key but the name.
function mcpServerToolCallsKey(
  organizationIdOrSlug: string,
  projectId: string,
  mcpServerId: string,
  windowDays: 14 | 30 | 90 = 14,
): readonly [string, string, string, string, number] {
  return [
    "gram-admin-mcp-server-tool-calls",
    organizationIdOrSlug,
    projectId,
    mcpServerId,
    windowDays,
  ] as const;
}

export function mcpServerToolCallsQuery(
  organizationIdOrSlug: string,
  request: McpServerHealthRequest,
): ReturnType<typeof createMcpServerToolCallsQuery> {
  return createMcpServerToolCallsQuery(organizationIdOrSlug, request);
}

function createMcpServerToolCallsQuery(
  organizationIdOrSlug: string,
  request: McpServerHealthRequest,
) {
  const generated = buildAdminGetMcpServerToolCallsQuery(
    redirectingClient,
    request,
  );
  return queryOptions({
    queryKey: mcpServerToolCallsKey(
      organizationIdOrSlug,
      request.projectId,
      request.mcpServerId,
      request.windowDays,
    ),
    queryFn: (context) => redirecting(generated.queryFn(context)),
    staleTime: 30_000,
  });
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

import { buildAdminOnboardingUseCasesQuery } from "@gram/admin-client/react-query/adminOnboardingUseCases.core";
import { buildCreateAdminOnboardingUseCaseMutation } from "@gram/admin-client/react-query/createAdminOnboardingUseCase";
import { buildUpdateAdminOnboardingUseCaseMutation } from "@gram/admin-client/react-query/updateAdminOnboardingUseCase";
import { buildDeleteAdminOnboardingUseCaseMutation } from "@gram/admin-client/react-query/deleteAdminOnboardingUseCase";
import { buildAdminOnboardingPlaybooksQuery } from "@gram/admin-client/react-query/adminOnboardingPlaybooks.core";
import { buildCreateAdminOnboardingPlaybookMutation } from "@gram/admin-client/react-query/createAdminOnboardingPlaybook";
import { buildUpdateAdminOnboardingPlaybookMutation } from "@gram/admin-client/react-query/updateAdminOnboardingPlaybook";
import { buildDeleteAdminOnboardingPlaybookMutation } from "@gram/admin-client/react-query/deleteAdminOnboardingPlaybook";
import { buildCloneAdminOnboardingPlaybookMutation } from "@gram/admin-client/react-query/cloneAdminOnboardingPlaybook";
import { buildAdminOrganizationOnboardingPlaybookQuery } from "@gram/admin-client/react-query/adminOrganizationOnboardingPlaybook.core";
import { buildAssignAdminOrganizationOnboardingPlaybookMutation } from "@gram/admin-client/react-query/assignAdminOrganizationOnboardingPlaybook";

// The stack form's options come from the support matrix catalog, which only
// changes on deploy, so the list is fetched once per session.
function createOnboardingStackOptionsQuery() {
  const generated = buildAdminOnboardingStackOptionsQuery(redirectingClient);
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    staleTime: Infinity,
  });
}

export function onboardingStackOptionsQuery(): ReturnType<
  typeof createOnboardingStackOptionsQuery
> {
  return createOnboardingStackOptionsQuery();
}

function createOrganizationOnboardingStackQuery(organizationId: string) {
  const generated = buildAdminOrganizationOnboardingStackQuery(
    redirectingClient,
    { organizationId },
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

export function organizationOnboardingStackQuery(
  organizationId: string,
): ReturnType<typeof createOrganizationOnboardingStackQuery> {
  return createOrganizationOnboardingStackQuery(organizationId);
}

const generatedOnboardingStackMutation =
  buildSetAdminOrganizationOnboardingStackMutation(mutationClient);

export function setAdminOrganizationOnboardingStack(
  request: SetOrganizationOnboardingStackRequestBody,
): Promise<AdminOnboardingStack> {
  return generatedOnboardingStackMutation.mutationFn({ request });
}

// Steps are defined in code and mirrored at start-up, so they only change on
// deploy.
function createOnboardingStepsQuery() {
  const generated = buildAdminOnboardingStepsQuery(redirectingClient);
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    staleTime: Infinity,
  });
}

export function onboardingStepsQuery(): ReturnType<
  typeof createOnboardingStepsQuery
> {
  return createOnboardingStepsQuery();
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
    workos_dashboard_url: org.workosDashboardUrl,
    stripe_customer_id: org.stripeCustomerId,
    stripe_subscription_id: org.stripeSubscriptionId,
    whitelisted: org.whitelisted,
    disabled_at: org.disabledAt?.toISOString(),
    trial_state: org.trialState,
    trial_ends_at: org.trialEndsAt?.toISOString(),
    trial_tier: org.trialTier,
    trial_converted_at: org.trialConvertedAt?.toISOString(),
    trial_demoted_at: org.trialDemotedAt?.toISOString(),
    creation_source: org.creationSource,
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
const setStripeSubscriptionMutation =
  buildAdminSetStripeSubscriptionMutation(redirectingClient);
const changeTrialEndDateMutation =
  buildAdminChangeTrialEndDateMutation(redirectingClient);

// Preview is a one-shot confirmation read of live Stripe state, not a
// typed-as-you-go query.
export function getStripeSubscriptionCandidate(
  request: AdminGetStripeSubscriptionCandidateRequest,
): Promise<AdminStripeSubscriptionCandidate> {
  return redirecting(
    unwrapAsync(
      adminGetStripeSubscriptionCandidate(redirectingClient, request),
    ),
  );
}

export async function setStripeSubscription(
  request: SetStripeSubscriptionRequestBody,
): Promise<AdminOrganization> {
  return organizationFromSdk(
    await redirecting(setStripeSubscriptionMutation.mutationFn({ request })),
  );
}

export async function changeTrialEndDate(
  request: ChangeTrialEndDateRequestBody,
): Promise<AdminOrganization> {
  return organizationFromSdk(
    await redirecting(changeTrialEndDateMutation.mutationFn({ request })),
  );
}

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

// Use cases and playbooks are edited in the admin dashboard, so their lists
// refetch like any other record.
function createOnboardingUseCasesQuery() {
  const generated = buildAdminOnboardingUseCasesQuery(redirectingClient);
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

export function onboardingUseCasesQuery(): ReturnType<
  typeof createOnboardingUseCasesQuery
> {
  return createOnboardingUseCasesQuery();
}

const generatedCreateUseCase =
  buildCreateAdminOnboardingUseCaseMutation(mutationClient);
const generatedUpdateUseCase =
  buildUpdateAdminOnboardingUseCaseMutation(mutationClient);
const generatedDeleteUseCase =
  buildDeleteAdminOnboardingUseCaseMutation(mutationClient);

export function createAdminOnboardingUseCase(
  request: Parameters<typeof generatedCreateUseCase.mutationFn>[0]["request"],
): ReturnType<typeof generatedCreateUseCase.mutationFn> {
  return generatedCreateUseCase.mutationFn({ request });
}

export function updateAdminOnboardingUseCase(
  request: Parameters<typeof generatedUpdateUseCase.mutationFn>[0]["request"],
): ReturnType<typeof generatedUpdateUseCase.mutationFn> {
  return generatedUpdateUseCase.mutationFn({ request });
}

export function deleteAdminOnboardingUseCase(
  request: Parameters<typeof generatedDeleteUseCase.mutationFn>[0]["request"],
): ReturnType<typeof generatedDeleteUseCase.mutationFn> {
  return generatedDeleteUseCase.mutationFn({ request });
}

function createOnboardingPlaybooksQuery(organizationId?: string) {
  const generated = buildAdminOnboardingPlaybooksQuery(redirectingClient, {
    organizationId,
  });
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

/** The default playbooks, plus the organization's custom ones when named. */
export function onboardingPlaybooksQuery(
  organizationId?: string,
): ReturnType<typeof createOnboardingPlaybooksQuery> {
  return createOnboardingPlaybooksQuery(organizationId);
}

const generatedCreatePlaybook =
  buildCreateAdminOnboardingPlaybookMutation(mutationClient);
const generatedUpdatePlaybook =
  buildUpdateAdminOnboardingPlaybookMutation(mutationClient);
const generatedDeletePlaybook =
  buildDeleteAdminOnboardingPlaybookMutation(mutationClient);
const generatedClonePlaybook =
  buildCloneAdminOnboardingPlaybookMutation(mutationClient);

export function createAdminOnboardingPlaybook(
  request: Parameters<typeof generatedCreatePlaybook.mutationFn>[0]["request"],
): ReturnType<typeof generatedCreatePlaybook.mutationFn> {
  return generatedCreatePlaybook.mutationFn({ request });
}

export function updateAdminOnboardingPlaybook(
  request: Parameters<typeof generatedUpdatePlaybook.mutationFn>[0]["request"],
): ReturnType<typeof generatedUpdatePlaybook.mutationFn> {
  return generatedUpdatePlaybook.mutationFn({ request });
}

export function deleteAdminOnboardingPlaybook(
  request: Parameters<typeof generatedDeletePlaybook.mutationFn>[0]["request"],
): ReturnType<typeof generatedDeletePlaybook.mutationFn> {
  return generatedDeletePlaybook.mutationFn({ request });
}

export function cloneAdminOnboardingPlaybook(
  request: Parameters<typeof generatedClonePlaybook.mutationFn>[0]["request"],
): ReturnType<typeof generatedClonePlaybook.mutationFn> {
  return generatedClonePlaybook.mutationFn({ request });
}

function createOrganizationOnboardingPlaybookQuery(organizationId: string) {
  const generated = buildAdminOrganizationOnboardingPlaybookQuery(
    redirectingClient,
    { organizationId },
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

export function organizationOnboardingPlaybookQuery(
  organizationId: string,
): ReturnType<typeof createOrganizationOnboardingPlaybookQuery> {
  return createOrganizationOnboardingPlaybookQuery(organizationId);
}

const generatedAssignPlaybook =
  buildAssignAdminOrganizationOnboardingPlaybookMutation(mutationClient);

export function assignAdminOrganizationOnboardingPlaybook(
  request: Parameters<typeof generatedAssignPlaybook.mutationFn>[0]["request"],
): ReturnType<typeof generatedAssignPlaybook.mutationFn> {
  return generatedAssignPlaybook.mutationFn({ request });
}

function createRegistryEntriesQuery(params: AdminListRegistryEntriesRequest) {
  const generated = buildAdminListRegistryEntriesQuery(
    redirectingClient,
    params,
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

function createRegistryEntryQuery(id: string) {
  const generated = buildAdminGetRegistryEntryQuery(redirectingClient, { id });
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    enabled: id !== "",
  });
}

export function useCreateRegistryEntryMutation(): UseMutationResult<
  AdminCreateRegistryEntryMutationData,
  AdminCreateRegistryEntryMutationError,
  AdminCreateRegistryEntryMutationVariables
> {
  return useMutation({
    ...buildAdminCreateRegistryEntryMutation(redirectingClient),
    retry: false,
  });
}

export function useSaveRegistryEntryMutation(): UseMutationResult<
  AdminSaveRegistryEntryMutationData,
  AdminSaveRegistryEntryMutationError,
  AdminSaveRegistryEntryMutationVariables
> {
  return useMutation({
    ...buildAdminSaveRegistryEntryMutation(redirectingClient),
    retry: false,
  });
}

export function useSetRegistryEntryPublishedMutation(): UseMutationResult<
  AdminSetRegistryEntryPublishedMutationData,
  AdminSetRegistryEntryPublishedMutationError,
  AdminSetRegistryEntryPublishedMutationVariables
> {
  return useMutation({
    ...buildAdminSetRegistryEntryPublishedMutation(redirectingClient),
    retry: false,
  });
}

export function registryEntriesQuery(
  params: AdminListRegistryEntriesRequest,
): ReturnType<typeof createRegistryEntriesQuery> {
  return createRegistryEntriesQuery(params);
}

export function registryEntryQuery(
  id: string,
): ReturnType<typeof createRegistryEntryQuery> {
  return createRegistryEntryQuery(id);
}

function createRegistryOktaCandidatesQuery(id: string) {
  const generated = buildAdminGetRegistryOktaCandidatesQuery(
    redirectingClient,
    { id },
  );
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    enabled: id !== "",
  });
}

export function registryOktaCandidatesQuery(
  id: string,
): ReturnType<typeof createRegistryOktaCandidatesQuery> {
  return createRegistryOktaCandidatesQuery(id);
}

function createRegistryOktaUnmappedQuery() {
  const generated = buildAdminListRegistryOktaUnmappedQuery(redirectingClient);
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

export function registryOktaUnmappedQuery(): ReturnType<
  typeof createRegistryOktaUnmappedQuery
> {
  return createRegistryOktaUnmappedQuery();
}
