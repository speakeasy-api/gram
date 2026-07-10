import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RenderTemplateResult } from "../models/components/rendertemplateresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RenderTemplateByIDRequest, RenderTemplateByIDSecurity } from "../models/operations/rendertemplatebyid.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * renderTemplateByID templates
 *
 * @remarks
 * Render a prompt template by ID with provided input data.
 */
export declare function templatesRenderByID(client: GramCore, request: RenderTemplateByIDRequest, security?: RenderTemplateByIDSecurity | undefined, options?: RequestOptions): APIPromise<Result<RenderTemplateResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=templatesRenderByID.d.ts.map