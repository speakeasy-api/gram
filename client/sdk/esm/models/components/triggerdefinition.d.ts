import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TriggerEnvRequirement } from "./triggerenvrequirement.js";
/**
 * The ingress kind for the trigger definition.
 */
export declare const TriggerDefinitionKind: {
    readonly Webhook: "webhook";
    readonly Schedule: "schedule";
};
/**
 * The ingress kind for the trigger definition.
 */
export type TriggerDefinitionKind = ClosedEnum<typeof TriggerDefinitionKind>;
export type TriggerDefinition = {
    /**
     * JSON schema describing the trigger config.
     */
    configSchema: string;
    /**
     * Description of the trigger definition.
     */
    description: string;
    /**
     * Environment variables required by this trigger definition.
     */
    envRequirements: Array<TriggerEnvRequirement>;
    /**
     * The ingress kind for the trigger definition.
     */
    kind: TriggerDefinitionKind;
    /**
     * The trigger definition slug.
     */
    slug: string;
    /**
     * The trigger definition title.
     */
    title: string;
};
/** @internal */
export declare const TriggerDefinitionKind$inboundSchema: z.ZodMiniEnum<typeof TriggerDefinitionKind>;
/** @internal */
export declare const TriggerDefinition$inboundSchema: z.ZodMiniType<TriggerDefinition, unknown>;
export declare function triggerDefinitionFromJSON(jsonString: string): SafeParseResult<TriggerDefinition, SDKValidationError>;
//# sourceMappingURL=triggerdefinition.d.ts.map