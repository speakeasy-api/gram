import * as z from "zod/v4-mini";
import { HookIngestData, HookIngestData$Outbound } from "./hookingestdata.js";
import {
  HookIngestEvent,
  HookIngestEvent$Outbound,
} from "./hookingestevent.js";
import {
  HookIngestSession,
  HookIngestSession$Outbound,
} from "./hookingestsession.js";
import {
  HookIngestSource,
  HookIngestSource$Outbound,
} from "./hookingestsource.js";
export type IngestRequestBody = {
  /**
   * Feature-specific payloads. Hooks populate only the blocks needed for the event.
   */
  data?: HookIngestData | undefined;
  /**
   * Canonical Gram feature event.
   */
  event: HookIngestEvent;
  /**
   * Original provider payload for debugging. The backend does not use this for feature behavior.
   */
  raw?: any | undefined;
  /**
   * Contract version. The current version is hook.ingest.v1.
   */
  schemaVersion: string;
  /**
   * Agent session and turn identity, independent of provider naming.
   */
  session?: HookIngestSession | undefined;
  /**
   * Metadata about the local hook adapter that translated a provider event into the Gram hook contract.
   */
  source: HookIngestSource;
};
/** @internal */
export type IngestRequestBody$Outbound = {
  data?: HookIngestData$Outbound | undefined;
  event: HookIngestEvent$Outbound;
  raw?: any | undefined;
  schema_version: string;
  session?: HookIngestSession$Outbound | undefined;
  source: HookIngestSource$Outbound;
};
/** @internal */
export declare const IngestRequestBody$outboundSchema: z.ZodMiniType<
  IngestRequestBody$Outbound,
  IngestRequestBody
>;
export declare function ingestRequestBodyToJSON(
  ingestRequestBody: IngestRequestBody,
): string;
//# sourceMappingURL=ingestrequestbody.d.ts.map
