import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskResultsByChatResult } from "../models/components/listriskresultsbychatresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListRiskResultsByChatRequest, ListRiskResultsByChatSecurity } from "../models/operations/listriskresultsbychat.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listRiskResultsByChat risk
 *
 * @remarks
 * List risk results grouped by chat session for the current project.
 */
export declare function riskResultsByChat(client: GramCore, request?: ListRiskResultsByChatRequest | undefined, security?: ListRiskResultsByChatSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListRiskResultsByChatResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskResultsByChat.d.ts.map