import * as z from "zod/v4-mini";
/**
 * Metadata about the local hook adapter that translated a provider event into the Gram hook contract.
 */
export type HookIngestSource = {
    /**
     * Stable adapter slug, e.g. claude, cursor, codex, or a customer hook name.
     */
    adapter: string;
    /**
     * Adapter implementation version.
     */
    adapterVersion?: string | undefined;
    /**
     * Hostname of the machine that emitted the hook event.
     */
    hostname?: string | undefined;
    /**
     * Provider-native event name, if one exists.
     */
    rawEventName?: string | undefined;
};
/** @internal */
export type HookIngestSource$Outbound = {
    adapter: string;
    adapter_version?: string | undefined;
    hostname?: string | undefined;
    raw_event_name?: string | undefined;
};
/** @internal */
export declare const HookIngestSource$outboundSchema: z.ZodMiniType<HookIngestSource$Outbound, HookIngestSource>;
export declare function hookIngestSourceToJSON(hookIngestSource: HookIngestSource): string;
//# sourceMappingURL=hookingestsource.d.ts.map