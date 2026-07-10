import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListIntegrationsResult } from "../models/components/listintegrationsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListIntegrationsRequest, ListIntegrationsSecurity } from "../models/operations/listintegrations.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * list integrations
 *
 * @remarks
 * List available third-party integrations.
 */
export declare function integrationsList(client: GramCore, request?: ListIntegrationsRequest | undefined, security?: ListIntegrationsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListIntegrationsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=integrationsList.d.ts.map