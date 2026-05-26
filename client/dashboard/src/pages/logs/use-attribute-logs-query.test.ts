import { describe, expect, it } from "vitest";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord";
import type { ToolCallSummary } from "@gram/client/models/components/toolcallsummary";
import {
  logsToTraceSummaries,
  mergeTraceSummariesByTraceId,
} from "./use-attribute-logs-query";

function makeLog({
  id,
  timeUnixNano,
  traceId,
  attributes,
}: {
  id: string;
  timeUnixNano: string;
  traceId?: string;
  attributes: Record<string, unknown>;
}): TelemetryLogRecord {
  return {
    id,
    body: "",
    timeUnixNano,
    observedTimeUnixNano: timeUnixNano,
    attributes,
    resourceAttributes: {},
    service: {
      name: "test-service",
    },
    traceId,
  };
}

describe("logsToTraceSummaries", () => {
  it("does not require a trigger event id for non-trigger groups", () => {
    const summaries = logsToTraceSummaries([
      makeLog({
        id: "late-log",
        timeUnixNano: "2",
        traceId: "trace-1",
        attributes: {
          gram: {
            event: { source: "tool_call" },
          },
        },
      }),
      makeLog({
        id: "early-log",
        timeUnixNano: "1",
        traceId: "trace-1",
        attributes: {
          gram: {
            tool: { urn: "urn:tool:test" },
          },
          http: {
            response: { status_code: 200 },
          },
        },
      }),
    ]);

    expect(summaries).toEqual([
      expect.objectContaining({
        traceId: "trace-1",
        gramUrn: "urn:tool:test",
        httpStatusCode: 200,
        eventSource: "tool_call",
      }),
    ]);
  });

  it("surfaces the failing status code when a trace mixes success and error logs", () => {
    const summaries = logsToTraceSummaries([
      makeLog({
        id: "ok-log",
        timeUnixNano: "1",
        traceId: "trace-mixed",
        attributes: {
          gram: {
            event: { source: "tool_call" },
            tool: { urn: "urn:tool:failing" },
          },
          http: { response: { status_code: 200 } },
        },
      }),
      makeLog({
        id: "err-log",
        timeUnixNano: "2",
        traceId: "trace-mixed",
        attributes: {
          http: { response: { status_code: 500 } },
        },
      }),
    ]);

    expect(summaries).toEqual([
      expect.objectContaining({
        traceId: "trace-mixed",
        httpStatusCode: 500,
      }),
    ]);
  });

  it("surfaces the error code regardless of log iteration order", () => {
    const summaries = logsToTraceSummaries([
      makeLog({
        id: "err-log",
        timeUnixNano: "2",
        traceId: "trace-err-first",
        attributes: {
          gram: { tool: { urn: "urn:tool:err-first" } },
          http: { response: { status_code: 500 } },
        },
      }),
      makeLog({
        id: "ok-log",
        timeUnixNano: "1",
        traceId: "trace-err-first",
        attributes: {
          http: { response: { status_code: 200 } },
        },
      }),
    ]);

    expect(summaries).toEqual([
      expect.objectContaining({
        traceId: "trace-err-first",
        httpStatusCode: 500,
      }),
    ]);
  });
});

describe("mergeTraceSummariesByTraceId", () => {
  it("surfaces the error code across paginated trace summaries", () => {
    // Simulates a single trace whose 200 handshake and 500 tool call land on
    // separate pages of the infinite query. The merge must keep the 500.
    const pageOne: ToolCallSummary = {
      traceId: "trace-paginated",
      gramUrn: "urn:tool:paginated",
      startTimeUnixNano: "2",
      logCount: 1,
      httpStatusCode: 200,
    };
    const pageTwo: ToolCallSummary = {
      traceId: "trace-paginated",
      gramUrn: "urn:tool:paginated",
      startTimeUnixNano: "1",
      logCount: 1,
      httpStatusCode: 500,
    };

    const merged = mergeTraceSummariesByTraceId([pageOne, pageTwo]);

    expect(merged).toEqual([
      expect.objectContaining({
        traceId: "trace-paginated",
        httpStatusCode: 500,
        logCount: 2,
        startTimeUnixNano: "1",
      }),
    ]);
  });

  it("retains a defined status when a later page has no code", () => {
    const merged = mergeTraceSummariesByTraceId([
      {
        traceId: "trace-no-status-on-second",
        gramUrn: "urn:tool:x",
        startTimeUnixNano: "2",
        logCount: 1,
        httpStatusCode: 500,
      },
      {
        traceId: "trace-no-status-on-second",
        gramUrn: "urn:tool:x",
        startTimeUnixNano: "1",
        logCount: 1,
      },
    ]);

    expect(merged[0]).toEqual(
      expect.objectContaining({ httpStatusCode: 500, logCount: 2 }),
    );
  });
});
