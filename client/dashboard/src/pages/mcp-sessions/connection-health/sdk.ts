import type { GramCore } from "@gram/client/core.js";
import { mcpConnectionHealthGetAttempt } from "@gram/client/funcs/mcpConnectionHealthGetAttempt.js";
import { mcpConnectionHealthGetAttemptCounts } from "@gram/client/funcs/mcpConnectionHealthGetAttemptCounts.js";
import { mcpConnectionHealthGetSessionActivity } from "@gram/client/funcs/mcpConnectionHealthGetSessionActivity.js";
import { mcpConnectionHealthGetSignInSummary } from "@gram/client/funcs/mcpConnectionHealthGetSignInSummary.js";
import { mcpConnectionHealthListAttempts } from "@gram/client/funcs/mcpConnectionHealthListAttempts.js";
import { mcpConnectionHealthListDeniedClients } from "@gram/client/funcs/mcpConnectionHealthListDeniedClients.js";
import type { ConnectionAttemptDetail } from "@gram/client/models/components/connectionattemptdetail.js";
import type { ConnectionAttemptFilters } from "@gram/client/models/components/connectionattemptfilters.js";
import type { ConnectionAttemptRow } from "@gram/client/models/components/connectionattemptrow.js";
import type { ConnectionAttemptStep } from "@gram/client/models/components/connectionattemptstep.js";
import type { ConnectionPerson } from "@gram/client/models/components/connectionperson.js";
import type { ConnectionServer } from "@gram/client/models/components/connectionserver.js";
import { unwrapAsync } from "@gram/client/types/fp";
import type {
  AttemptCountsResponse,
  AttemptDetail,
  AttemptFilters,
  AttemptRangeRequest,
  AttemptRow,
  AttemptStep,
  DeniedClientsResponse,
  GetAttemptResponse,
  ListAttemptsRequest,
  ListAttemptsResponse,
  Outcome,
  Person,
  ServerKind,
  ServerRef,
  SessionActivityResponse,
  SignInSummaryResponse,
  Source,
  StepGroup,
  SummaryBucket,
  ViewerScope,
} from "./types";

// The SDK names enum fields after their types (`connectionOutcome`), keeps
// times as Dates and leaves enums open; the views read the contract's names.

const iso = (date: Date): string => date.toISOString();

const RETENTION_MS = 30 * 24 * 60 * 60 * 1000;

/** A window start no older than retention by the time it is sent. */
function since(from: string): Date {
  const oldest = Date.now() - RETENTION_MS + 60_000;
  return new Date(Math.max(Date.parse(from), oldest));
}

function server(ref: ConnectionServer | undefined): ServerRef | undefined {
  return ref && { id: ref.id, kind: ref.kind as ServerKind, name: ref.name };
}

function person(ref: ConnectionPerson | undefined): Person | undefined {
  return (
    ref && {
      subjectUrn: ref.subjectUrn,
      kind: ref.kind as Person["kind"],
      userId: ref.userId,
      name: ref.name,
      email: ref.email,
    }
  );
}

function row(r: ConnectionAttemptRow | ConnectionAttemptDetail): AttemptRow {
  return {
    id: r.id,
    time: iso(r.time),
    firstStartedAt: iso(r.firstStartedAt),
    count: r.count,
    outcome: r.connectionOutcome as Outcome,
    step: r.connectionStepGroup as StepGroup,
    reason: r.reason,
    message: r.message,
    technicalMessage: r.technicalMessage,
    source: r.connectionFailureSource as Source | undefined,
    person: person(r.person),
    network: r.network && { key: r.network.key, label: r.network.label },
    app: {
      family: r.app.family,
      name: r.app.name,
      version: r.app.version,
      appKey: r.app.appKey,
    },
    server: server(r.server),
    member: server(r.member),
    service: r.service,
  };
}

function step(s: ConnectionAttemptStep): AttemptStep {
  return {
    at: iso(s.at),
    step: s.connectionStepGroup as StepGroup,
    storedStep: s.storedStep,
    result: s.result,
    final: s.final,
    reason: s.reason,
    message: s.message,
    technicalMessage: s.technicalMessage,
    member: server(s.member),
    service: s.service,
  };
}

function detail(d: ConnectionAttemptDetail): AttemptDetail {
  return {
    ...row(d),
    startedAt: iso(d.startedAt),
    userSessionIssuerId: d.userSessionIssuerId,
    traceId: d.traceId,
    steps: d.steps.map(step),
    context: {
      grantType: d.context.grantType,
      subReason: d.context.subReason,
      retries: d.context.retries,
    },
  };
}

function filters(f: AttemptFilters | undefined): ConnectionAttemptFilters {
  return f as ConnectionAttemptFilters;
}

function range(request: AttemptRangeRequest) {
  return {
    userSessionIssuerId: request.userSessionIssuerId,
    serverId: request.serverId,
    from: since(request.from),
    to: new Date(request.to),
    filters: filters(request.filters),
  };
}

export async function listAttempts(
  client: GramCore,
  gramProject: string,
  request: ListAttemptsRequest,
): Promise<ListAttemptsResponse> {
  const result = await unwrapAsync(
    mcpConnectionHealthListAttempts(client, {
      gramProject,
      listAttemptsRequestBody: {
        ...range(request),
        cursor: request.cursor,
        limit: request.limit,
      },
    }),
  );
  return {
    loggingEnabled: result.loggingEnabled,
    viewerScope: result.connectionViewerScope as ViewerScope,
    attempts: result.attempts?.map(row),
    nextCursor: result.nextCursor,
  };
}

export async function getAttemptCounts(
  client: GramCore,
  gramProject: string,
  request: AttemptRangeRequest,
): Promise<AttemptCountsResponse> {
  const result = await unwrapAsync(
    mcpConnectionHealthGetAttemptCounts(client, {
      gramProject,
      connectionAttemptQuery: range(request),
    }),
  );
  return {
    loggingEnabled: result.loggingEnabled,
    viewerScope: result.connectionViewerScope as ViewerScope,
    totalAttempts: result.totalAttempts,
    facets: result.facets,
    truncated: result.truncated,
  };
}

export async function getAttempt(
  client: GramCore,
  gramProject: string,
  reference: string,
): Promise<GetAttemptResponse> {
  const result = await unwrapAsync(
    mcpConnectionHealthGetAttempt(client, {
      gramProject,
      getAttemptRequestBody: { reference },
    }),
  );
  return {
    loggingEnabled: result.loggingEnabled,
    viewerScope: result.connectionViewerScope as ViewerScope,
    found: result.found,
    attempt: result.attempt && detail(result.attempt),
  };
}

export async function getSignInSummary(
  client: GramCore,
  gramProject: string,
  request: {
    userSessionIssuerId: string;
    serverId?: string;
    from: string;
    to: string;
    bucket?: SummaryBucket;
  },
): Promise<SignInSummaryResponse> {
  const result = await unwrapAsync(
    mcpConnectionHealthGetSignInSummary(client, {
      gramProject,
      getSignInSummaryRequestBody: {
        userSessionIssuerId: request.userSessionIssuerId,
        serverId: request.serverId,
        from: since(request.from),
        to: new Date(request.to),
        bucket: request.bucket,
      },
    }),
  );
  return {
    loggingEnabled: result.loggingEnabled,
    viewerScope: result.connectionViewerScope as ViewerScope,
    bucket: result.bucket as SummaryBucket | undefined,
    buckets: result.buckets?.map((b) => ({
      start: iso(b.start),
      signIns: b.signIns,
      renewals: b.renewals,
    })),
  };
}

export async function getSessionActivity(
  client: GramCore,
  gramProject: string,
  request: { userSessionIssuerId: string; from: string; to: string },
): Promise<SessionActivityResponse> {
  const result = await unwrapAsync(
    mcpConnectionHealthGetSessionActivity(client, {
      gramProject,
      getSessionActivityRequestBody: {
        userSessionIssuerId: request.userSessionIssuerId,
        from: since(request.from),
        to: new Date(request.to),
      },
    }),
  );
  return {
    days: result.days,
    activeNow: result.activeNow,
    averageSessionSeconds: result.averageSessionSeconds,
    averageWindowFrom: iso(result.averageWindowFrom),
    averageWindowTo: iso(result.averageWindowTo),
    configuredSessionSeconds: result.configuredSessionSeconds,
  };
}

export async function listDeniedClients(
  client: GramCore,
  gramProject: string,
  request: {
    userSessionIssuerId: string;
    serverId?: string;
    from: string;
    to: string;
  },
): Promise<DeniedClientsResponse> {
  const result = await unwrapAsync(
    mcpConnectionHealthListDeniedClients(client, {
      gramProject,
      connectionHealthRangeQuery: {
        userSessionIssuerId: request.userSessionIssuerId,
        serverId: request.serverId,
        from: since(request.from),
        to: new Date(request.to),
      },
    }),
  );
  return {
    loggingEnabled: result.loggingEnabled,
    viewerScope: result.connectionViewerScope as ViewerScope,
    clients: result.clients?.map((c) => ({
      app: c.app,
      reason: c.reason,
      message: c.message,
      technicalMessage: c.technicalMessage,
      attempts: c.attempts,
      people: c.people,
      networks: c.networks,
      lastAt: iso(c.lastAt),
    })),
  };
}
