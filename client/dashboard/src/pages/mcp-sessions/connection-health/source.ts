import type {
  ActiveKillswitch,
  AttemptCountsResponse,
  AttemptRangeRequest,
  DeniedClientsResponse,
  GetAttemptResponse,
  GetSuspectedProblemResponse,
  ListAttemptsRequest,
  ListAttemptsResponse,
  ListSuspectedProblemsResponse,
  SessionActivityResponse,
  SignInSummaryResponse,
  SummaryBucket,
} from "./types";

/** Every mcpConnectionHealth read the views make; `api.ts` picks the implementation. */
export interface ConnectionHealthSource {
  listAttempts(request: ListAttemptsRequest): Promise<ListAttemptsResponse>;
  getAttemptCounts(
    request: AttemptRangeRequest,
  ): Promise<AttemptCountsResponse>;
  getAttempt(request: { reference: string }): Promise<GetAttemptResponse>;
  getSignInSummary(request: {
    userSessionIssuerId: string;
    serverId?: string;
    from: string;
    to: string;
    bucket?: SummaryBucket;
  }): Promise<SignInSummaryResponse>;
  getSessionActivity(request: {
    userSessionIssuerId: string;
    from: string;
    to: string;
  }): Promise<SessionActivityResponse>;
  listDeniedClients(request: {
    userSessionIssuerId: string;
    serverId?: string;
    from: string;
    to: string;
  }): Promise<DeniedClientsResponse>;
  listSuspectedProblems(request: {
    userSessionIssuerId: string;
    serverId?: string;
  }): Promise<ListSuspectedProblemsResponse>;
  getSuspectedProblem(request: {
    id: string;
  }): Promise<GetSuspectedProblemResponse>;
  /** Not an mcpConnectionHealth method; see ActiveKillswitch. */
  listActiveKillswitches(request: {
    userSessionIssuerId: string;
  }): Promise<ActiveKillswitch[]>;
}
