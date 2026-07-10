import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SetProjectLogoResult } from "../models/components/setprojectlogoresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { SetProjectLogoRequest, SetProjectLogoSecurity } from "../models/operations/setprojectlogo.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * setLogo projects
 *
 * @remarks
 * Uploads a logo for a project.
 */
export declare function projectsSetLogo(client: GramCore, request: SetProjectLogoRequest, security?: SetProjectLogoSecurity | undefined, options?: RequestOptions): APIPromise<Result<SetProjectLogoResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=projectsSetLogo.d.ts.map