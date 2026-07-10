import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { CaptureEventResult } from "../models/components/captureeventresult.js";
import { GetEmployeeDataFlowGraphResult } from "../models/components/getemployeedataflowgraphresult.js";
import { GetHooksSummaryResult } from "../models/components/gethookssummaryresult.js";
import { GetMetricsSummaryResult } from "../models/components/getmetricssummaryresult.js";
import { GetObservabilityOverviewResult } from "../models/components/getobservabilityoverviewresult.js";
import { GetProjectOverviewResult } from "../models/components/getprojectoverviewresult.js";
import { GetToolUsageFilterOptionsResult } from "../models/components/gettoolusagefilteroptionsresult.js";
import { GetToolUsageSummaryResult } from "../models/components/gettoolusagesummaryresult.js";
import { GetUserMetricsSummaryResult } from "../models/components/getusermetricssummaryresult.js";
import { ListAttributeKeysResult } from "../models/components/listattributekeysresult.js";
import { ListFilterOptionsResult } from "../models/components/listfilteroptionsresult.js";
import { ListHooksTracesResult } from "../models/components/listhookstracesresult.js";
import { ListSessionsResult } from "../models/components/listsessionsresult.js";
import { ListToolUsageTracesResult } from "../models/components/listtoolusagetracesresult.js";
import { QueryResult } from "../models/components/queryresult.js";
import { SearchChatsResult } from "../models/components/searchchatsresult.js";
import { SearchLogsResult } from "../models/components/searchlogsresult.js";
import { SearchToolCallsResult } from "../models/components/searchtoolcallsresult.js";
import { SearchUsersResult } from "../models/components/searchusersresult.js";
import { CaptureEventRequest, CaptureEventSecurity } from "../models/operations/captureevent.js";
import { GetEmployeeDataFlowGraphRequest, GetEmployeeDataFlowGraphSecurity } from "../models/operations/getemployeedataflowgraph.js";
import { GetHooksSummaryRequest, GetHooksSummarySecurity } from "../models/operations/gethookssummary.js";
import { GetObservabilityOverviewRequest, GetObservabilityOverviewSecurity } from "../models/operations/getobservabilityoverview.js";
import { GetProjectMetricsSummaryRequest, GetProjectMetricsSummarySecurity } from "../models/operations/getprojectmetricssummary.js";
import { GetProjectOverviewRequest, GetProjectOverviewSecurity } from "../models/operations/getprojectoverview.js";
import { GetToolUsageFilterOptionsRequest, GetToolUsageFilterOptionsSecurity } from "../models/operations/gettoolusagefilteroptions.js";
import { GetToolUsageSummaryRequest, GetToolUsageSummarySecurity } from "../models/operations/gettoolusagesummary.js";
import { GetUserMetricsSummaryRequest, GetUserMetricsSummarySecurity } from "../models/operations/getusermetricssummary.js";
import { ListAttributeKeysRequest, ListAttributeKeysSecurity } from "../models/operations/listattributekeys.js";
import { ListFilterOptionsRequest, ListFilterOptionsSecurity } from "../models/operations/listfilteroptions.js";
import { ListHooksTracesRequest, ListHooksTracesSecurity } from "../models/operations/listhookstraces.js";
import { ListSessionsRequest, ListSessionsSecurity } from "../models/operations/listsessions.js";
import { ListToolUsageTracesRequest, ListToolUsageTracesSecurity } from "../models/operations/listtoolusagetraces.js";
import { QueryRequest, QuerySecurity } from "../models/operations/query.js";
import { SearchChatsRequest, SearchChatsSecurity } from "../models/operations/searchchats.js";
import { SearchLogsRequest, SearchLogsSecurity } from "../models/operations/searchlogs.js";
import { SearchToolCallsRequest, SearchToolCallsSecurity } from "../models/operations/searchtoolcalls.js";
import { SearchUsersRequest, SearchUsersSecurity } from "../models/operations/searchusers.js";
export declare class Telemetry extends ClientSDK {
    /**
     * captureEvent telemetry
     *
     * @remarks
     * Capture a telemetry event and forward it to PostHog
     */
    captureEvent(request: CaptureEventRequest, security?: CaptureEventSecurity | undefined, options?: RequestOptions): Promise<CaptureEventResult>;
    /**
     * getEmployeeDataFlowGraph telemetry
     *
     * @remarks
     * Get an employee's MCP data flow graph across origins, clients, servers, and tools
     */
    getEmployeeDataFlowGraph(request: GetEmployeeDataFlowGraphRequest, security?: GetEmployeeDataFlowGraphSecurity | undefined, options?: RequestOptions): Promise<GetEmployeeDataFlowGraphResult>;
    /**
     * getHooksSummary telemetry
     *
     * @remarks
     * Get aggregated hooks metrics grouped by server
     */
    getHooksSummary(request: GetHooksSummaryRequest, security?: GetHooksSummarySecurity | undefined, options?: RequestOptions): Promise<GetHooksSummaryResult>;
    /**
     * getObservabilityOverview telemetry
     *
     * @remarks
     * Get observability overview metrics including time series, tool breakdowns, and summary stats
     */
    getObservabilityOverview(request: GetObservabilityOverviewRequest, security?: GetObservabilityOverviewSecurity | undefined, options?: RequestOptions): Promise<GetObservabilityOverviewResult>;
    /**
     * getProjectMetricsSummary telemetry
     *
     * @remarks
     * Get aggregated metrics summary for an entire project
     */
    getProjectMetricsSummary(request: GetProjectMetricsSummaryRequest, security?: GetProjectMetricsSummarySecurity | undefined, options?: RequestOptions): Promise<GetMetricsSummaryResult>;
    /**
     * getProjectOverview telemetry
     *
     * @remarks
     * Get project-level overview including total chats, tool calls, active servers/users, and top lists
     */
    getProjectOverview(request: GetProjectOverviewRequest, security?: GetProjectOverviewSecurity | undefined, options?: RequestOptions): Promise<GetProjectOverviewResult>;
    /**
     * getToolUsageFilterOptions telemetry
     *
     * @remarks
     * Get filter options for target-aware MCP and tool usage metrics
     */
    getToolUsageFilterOptions(request: GetToolUsageFilterOptionsRequest, security?: GetToolUsageFilterOptionsSecurity | undefined, options?: RequestOptions): Promise<GetToolUsageFilterOptionsResult>;
    /**
     * getToolUsageSummary telemetry
     *
     * @remarks
     * Get target-aware MCP and tool usage metrics
     */
    getToolUsageSummary(request: GetToolUsageSummaryRequest, security?: GetToolUsageSummarySecurity | undefined, options?: RequestOptions): Promise<GetToolUsageSummaryResult>;
    /**
     * getUserMetricsSummary telemetry
     *
     * @remarks
     * Get aggregated metrics summary grouped by user
     */
    getUserMetricsSummary(request: GetUserMetricsSummaryRequest, security?: GetUserMetricsSummarySecurity | undefined, options?: RequestOptions): Promise<GetUserMetricsSummaryResult>;
    /**
     * listAttributeKeys telemetry
     *
     * @remarks
     * List distinct attribute keys available for filtering
     */
    listAttributeKeys(request: ListAttributeKeysRequest, security?: ListAttributeKeysSecurity | undefined, options?: RequestOptions): Promise<ListAttributeKeysResult>;
    /**
     * listFilterOptions telemetry
     *
     * @remarks
     * List available filter options (API keys or users) for the observability overview
     */
    listFilterOptions(request: ListFilterOptionsRequest, security?: ListFilterOptionsSecurity | undefined, options?: RequestOptions): Promise<ListFilterOptionsResult>;
    /**
     * listHooksTraces telemetry
     *
     * @remarks
     * List hook traces aggregated by trace_id with user information
     */
    listHooksTraces(request: ListHooksTracesRequest, security?: ListHooksTracesSecurity | undefined, options?: RequestOptions): Promise<ListHooksTracesResult>;
    /**
     * listSessions telemetry
     *
     * @remarks
     * Org-scoped list of individual chat sessions for a slice of usage, filtered by the same allowlisted dimensions as telemetry.query. Returns per-session cost, token, and tool metrics with cursor pagination.
     */
    listSessions(request: ListSessionsRequest, security?: ListSessionsSecurity | undefined, options?: RequestOptions): Promise<ListSessionsResult>;
    /**
     * listToolUsageTraces telemetry
     *
     * @remarks
     * List target-aware MCP and tool usage traces
     */
    listToolUsageTraces(request: ListToolUsageTracesRequest, security?: ListToolUsageTracesSecurity | undefined, options?: RequestOptions): Promise<ListToolUsageTracesResult>;
    /**
     * query telemetry
     *
     * @remarks
     * Generic, org-scoped analytics query over pre-aggregated usage metrics. Returns both a grouped table and a per-group hourly timeseries for the same slice of data, supporting arbitrary allowlisted group-by dimensions and filters (e.g. group by department_name, then drill in by filtering department_name and grouping by role).
     */
    query(request: QueryRequest, security?: QuerySecurity | undefined, options?: RequestOptions): Promise<QueryResult>;
    /**
     * searchChats telemetry
     *
     * @remarks
     * Search and list chat session summaries that match a search filter
     */
    searchChats(request: SearchChatsRequest, security?: SearchChatsSecurity | undefined, options?: RequestOptions): Promise<SearchChatsResult>;
    /**
     * searchLogs telemetry
     *
     * @remarks
     * Search and list telemetry logs that match a search filter
     */
    searchLogs(request: SearchLogsRequest, security?: SearchLogsSecurity | undefined, options?: RequestOptions): Promise<SearchLogsResult>;
    /**
     * searchToolCalls telemetry
     *
     * @remarks
     * Search and list tool calls that match a search filter
     */
    searchToolCalls(request: SearchToolCallsRequest, security?: SearchToolCallsSecurity | undefined, options?: RequestOptions): Promise<SearchToolCallsResult>;
    /**
     * searchUsers telemetry
     *
     * @remarks
     * Search and list user usage summaries grouped by user_id or external_user_id
     */
    searchUsers(request: SearchUsersRequest, security?: SearchUsersSecurity | undefined, options?: RequestOptions): Promise<SearchUsersResult>;
}
//# sourceMappingURL=telemetry.d.ts.map