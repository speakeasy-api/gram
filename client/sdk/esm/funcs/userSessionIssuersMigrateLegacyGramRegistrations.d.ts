import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { MigrateLegacyGramRegistrationsResult } from "../models/components/migratelegacygramregistrationsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  MigrateLegacyGramRegistrationsRequest,
  MigrateLegacyGramRegistrationsSecurity,
} from "../models/operations/migratelegacygramregistrations.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * migrateLegacyGramRegistrations userSessionIssuers
 *
 * @remarks
 * One-off migration: lift the legacy Redis dynamic-client registrations from a gram-type oauth_proxy_provider into user_session_clients on the given user_session_issuer, so migrated MCP clients skip re-registration and re-auth. Removed once the OAuth proxy is retired.
 */
export declare function userSessionIssuersMigrateLegacyGramRegistrations(
  client: GramCore,
  request: MigrateLegacyGramRegistrationsRequest,
  security?: MigrateLegacyGramRegistrationsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    MigrateLegacyGramRegistrationsResult,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=userSessionIssuersMigrateLegacyGramRegistrations.d.ts.map
