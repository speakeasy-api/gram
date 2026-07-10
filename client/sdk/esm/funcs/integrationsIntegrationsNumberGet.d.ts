import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetIntegrationResult } from "../models/components/getintegrationresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { IntegrationsNumberGetRequest, IntegrationsNumberGetSecurity } from "../models/operations/integrationsnumberget.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * get integrations
 *
 * @remarks
 * Get a third-party integration by ID or name.
 */
export declare function integrationsIntegrationsNumberGet(client: GramCore, request?: IntegrationsNumberGetRequest | undefined, security?: IntegrationsNumberGetSecurity | undefined, options?: RequestOptions): APIPromise<Result<GetIntegrationResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=integrationsIntegrationsNumberGet.d.ts.map