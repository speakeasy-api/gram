import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { MigrateLegacyGramRegistrationsResult } from "../models/components/migratelegacygramregistrationsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { MigrateLegacyGramRegistrationsRequest, MigrateLegacyGramRegistrationsSecurity } from "../models/operations/migratelegacygramregistrations.js";
import { MutationHookOptions } from "./_types.js";
export type MigrateLegacyGramRegistrationsMutationVariables = {
    request: MigrateLegacyGramRegistrationsRequest;
    security?: MigrateLegacyGramRegistrationsSecurity | undefined;
    options?: RequestOptions;
};
export type MigrateLegacyGramRegistrationsMutationData = MigrateLegacyGramRegistrationsResult;
export type MigrateLegacyGramRegistrationsMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * migrateLegacyGramRegistrations userSessionIssuers
 *
 * @remarks
 * One-off migration: lift the legacy Redis dynamic-client registrations from a gram-type oauth_proxy_provider into user_session_clients on the given user_session_issuer, so migrated MCP clients skip re-registration and re-auth. Removed once the OAuth proxy is retired.
 */
export declare function useMigrateLegacyGramRegistrationsMutation(options?: MutationHookOptions<MigrateLegacyGramRegistrationsMutationData, MigrateLegacyGramRegistrationsMutationError, MigrateLegacyGramRegistrationsMutationVariables>): UseMutationResult<MigrateLegacyGramRegistrationsMutationData, MigrateLegacyGramRegistrationsMutationError, MigrateLegacyGramRegistrationsMutationVariables>;
export declare function mutationKeyMigrateLegacyGramRegistrations(): MutationKey;
export declare function buildMigrateLegacyGramRegistrationsMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: MigrateLegacyGramRegistrationsMutationVariables) => Promise<MigrateLegacyGramRegistrationsMutationData>;
};
//# sourceMappingURL=migrateLegacyGramRegistrations.d.ts.map