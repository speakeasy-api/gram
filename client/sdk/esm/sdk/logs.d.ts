import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export declare class Logs extends ClientSDK {
  /**
   * listLogs logs
   *
   * @remarks
   * List call logs for a toolset.
   */
  list(
    request?: operations.ListToolLogsRequest | undefined,
    security?: operations.ListToolLogsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.ListToolLogResponse>;
  /**
   * listToolExecutionLogs logs
   *
   * @remarks
   * List structured logs from tool executions.
   */
  listToolExecutionLogs(
    request?: operations.ListToolExecutionLogsRequest | undefined,
    security?: operations.ListToolExecutionLogsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.ListToolExecutionLogsResult>;
}
//# sourceMappingURL=logs.d.ts.map
