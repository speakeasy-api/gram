import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type AuditLog = {
  action: string;
  actorDisplayName?: string | undefined;
  actorId: string;
  actorSlug?: string | undefined;
  actorType: string;
  afterSnapshot?: any | undefined;
  beforeSnapshot?: any | undefined;
  /**
   * The creation date of the audit log.
   */
  createdAt: Date;
  id: string;
  metadata?:
    | {
        [k: string]: any;
      }
    | undefined;
  projectId?: string | undefined;
  projectSlug?: string | undefined;
  subjectDisplayName?: string | undefined;
  subjectId: string;
  subjectSlug?: string | undefined;
  subjectType: string;
};
/** @internal */
export declare const AuditLog$inboundSchema: z.ZodMiniType<AuditLog, unknown>;
export declare function auditLogFromJSON(
  jsonString: string,
): SafeParseResult<AuditLog, SDKValidationError>;
//# sourceMappingURL=auditlog.d.ts.map
