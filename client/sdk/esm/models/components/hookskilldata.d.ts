import * as z from "zod/v4-mini";
/**
 * Skill activation payload.
 */
export type HookSkillData = {
    /**
     * Activated skill name.
     */
    name: string;
    /**
     * Skill source or namespace, if available.
     */
    source?: string | undefined;
};
/** @internal */
export type HookSkillData$Outbound = {
    name: string;
    source?: string | undefined;
};
/** @internal */
export declare const HookSkillData$outboundSchema: z.ZodMiniType<HookSkillData$Outbound, HookSkillData>;
export declare function hookSkillDataToJSON(hookSkillData: HookSkillData): string;
//# sourceMappingURL=hookskilldata.d.ts.map