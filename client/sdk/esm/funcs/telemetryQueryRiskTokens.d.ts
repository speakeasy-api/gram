import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { QueryRiskTokensResult } from "../models/components/queryrisktokensresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { QueryRiskTokensRequest, QueryRiskTokensSecurity } from "../models/operations/queryrisktokens.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * queryRiskTokens telemetry
 *
 * @remarks
 * Org-scoped daily token usage split by risk involvement: tokens from sessions with at least one active risk finding in the window versus all session tokens. Powers the token-usage panel's risk breakdown on the costs page.
 */
export declare function telemetryQueryRiskTokens(client: GramCore, request: QueryRiskTokensRequest, security?: QueryRiskTokensSecurity | undefined, options?: RequestOptions): APIPromise<Result<QueryRiskTokensResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=telemetryQueryRiskTokens.d.ts.map