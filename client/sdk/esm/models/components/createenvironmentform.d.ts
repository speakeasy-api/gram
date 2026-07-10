import * as z from "zod/v4-mini";
import {
  EnvironmentEntryInput,
  EnvironmentEntryInput$Outbound,
} from "./environmententryinput.js";
/**
 * Form for creating a new environment
 */
export type CreateEnvironmentForm = {
  /**
   * Optional description of the environment
   */
  description?: string | undefined;
  /**
   * List of environment variable entries
   */
  entries: Array<EnvironmentEntryInput>;
  /**
   * The name of the environment
   */
  name: string;
  /**
   * The organization ID this environment belongs to
   */
  organizationId: string;
};
/** @internal */
export type CreateEnvironmentForm$Outbound = {
  description?: string | undefined;
  entries: Array<EnvironmentEntryInput$Outbound>;
  name: string;
  organization_id: string;
};
/** @internal */
export declare const CreateEnvironmentForm$outboundSchema: z.ZodMiniType<
  CreateEnvironmentForm$Outbound,
  CreateEnvironmentForm
>;
export declare function createEnvironmentFormToJSON(
  createEnvironmentForm: CreateEnvironmentForm,
): string;
//# sourceMappingURL=createenvironmentform.d.ts.map
