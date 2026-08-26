import { createActor } from "xstate";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  LISTEN_TIMEOUT_SECONDS,
  PROJECT_GUIDE_OUTPUT_LIMIT,
  getProjectGuideCurrentStep,
  projectGuideMachine,
  type ProjectGuideEventCard,
  type ProjectGuideOperationReport,
  type ProjectGuideOperationScope,
  type ProjectGuideOperationSignal,
} from "./projectGuideMachine";

function coordinator() {
  const signals: ProjectGuideOperationSignal[] = [];
  const service = createActor(projectGuideMachine, {
    input: {
      onSignal: (signal) => {
        signals.push(signal);
      },
    },
  }).start();

  return { service, signals };
}

function openMcp(service: ReturnType<typeof coordinator>["service"]): void {
  service.send({ type: "OPEN", path: "third-party-mcp", resumeStep: 0 });
}

function openSecret(
  service: ReturnType<typeof coordinator>["service"],
  resumeStep = 0,
): void {
  service.send({ type: "OPEN", path: "secret-block", resumeStep });
}

function latestScope(
  signals: ProjectGuideOperationSignal[],
): ProjectGuideOperationScope {
  const signal = signals.at(-1);
  if (!signal) throw new Error("expected operation signal");
  return signal.scope;
}

function report(
  service: ReturnType<typeof coordinator>["service"],
  report: ProjectGuideOperationReport,
): void {
  service.send({ type: "ADAPTER_REPORT", report });
}

function reachCheckpoint(
  service: ReturnType<typeof coordinator>["service"],
  signals: ProjectGuideOperationSignal[],
): void {
  openMcp(service);
  service.send({ type: "START" });
  report(service, {
    type: "success",
    scope: latestScope(signals),
    result: "Server installed",
  });
  if (service.getSnapshot().value === "preparing") {
    report(service, {
      type: "success",
      scope: latestScope(signals),
      result: "MCP ready",
    });
  }
  service.send({
    type: "USER_CHECKPOINT_COMPLETE",
    result: "Endpoint verified",
  });
}

function reachWaiting(
  service: ReturnType<typeof coordinator>["service"],
  signals: ProjectGuideOperationSignal[],
): void {
  reachCheckpoint(service, signals);
  service.send({
    type: "USER_CHECKPOINT_COMPLETE",
    result: "Client connected",
  });
  service.send({ type: "USER_CHECKPOINT_COMPLETE", result: "Prompt sent" });
}

const observedEvent: ProjectGuideEventCard = {
  kind: "Governed call",
  tone: "allow",
  title: "tools/list",
  rows: [{ key: "actor", value: "agent" }],
  note: "kept on the record",
};

afterEach(() => {
  vi.useRealTimers();
});

describe("project guide coordinator contract", () => {
  it("does not start work until the opened journey receives START", () => {
    const { service, signals } = coordinator();
    openMcp(service);

    expect(service.getSnapshot().value).toBe("ready");
    expect(signals).toEqual([]);

    service.send({ type: "START" });

    expect(service.getSnapshot().value).toBe("running");
    expect(signals).toEqual([
      {
        type: "start",
        scope: { path: "third-party-mcp", step: 0, attempt: 0, runId: 1 },
      },
    ]);
    expect(service.getSnapshot().context.output.at(-1)).toMatchObject({
      kind: "start",
      message: "Starting…",
    });
  });

  it("uses a concise ready message when a journey has not started", () => {
    const { service } = coordinator();

    openMcp(service);
    expect(service.getSnapshot().context.output.at(-1)).toMatchObject({
      kind: "note",
      message: "Ready to start",
    });

    service.send({ type: "SWITCH", path: "secret-block", resumeStep: 0 });
    expect(service.getSnapshot().context.output.at(-1)).toMatchObject({
      kind: "note",
      message: "Ready to start",
    });
  });

  it("records MCP selection and ignores selection changes during work", () => {
    const { service, signals } = coordinator();
    openMcp(service);

    service.send({ type: "SELECT_MCP_SERVER", name: "Linear" });
    expect(service.getSnapshot().context.output.at(-1)).toMatchObject({
      kind: "note",
      message: "Linear selected. Ready to start the journey",
    });

    service.send({ type: "START" });
    service.send({ type: "SELECT_MCP_SERVER", name: "Notion" });

    expect(service.getSnapshot().context.output.at(-1)?.message).not.toBe(
      "Notion selected. Ready to start the journey",
    );
    expect(signals).toHaveLength(1);
  });

  it("rebuilds the journey narrative at its resume point", () => {
    const { service } = coordinator();

    service.send({ type: "OPEN", path: "third-party-mcp", resumeStep: 2 });

    expect(
      service.getSnapshot().context.output.map((entry) => entry.message),
    ).toEqual([
      "Server selected",
      "Next · Connect your agent to this server",
      "Endpoint verified",
      "Next · Prompt agent to list the tools",
      "Ready · Prompt agent to list the tools",
    ]);
  });

  it("records automated progress and caps visible output history", () => {
    const { service, signals } = coordinator();
    openMcp(service);
    service.send({ type: "START" });

    for (let index = 0; index < PROJECT_GUIDE_OUTPUT_LIMIT + 4; index++) {
      report(service, {
        type: "progress",
        scope: latestScope(signals),
        message: `progress ${index}`,
        progress: index / 100,
      });
    }

    const { context } = service.getSnapshot();
    expect(context.output).toHaveLength(PROJECT_GUIDE_OUTPUT_LIMIT);
    expect(context.output.at(-1)?.kind).toBe("working");
    expect(context.output[0]?.message).toBe("progress 4");
    expect(context.output.at(-1)?.message).toBe(
      `progress ${PROJECT_GUIDE_OUTPUT_LIMIT + 3}`,
    );
    expect(context.operationProgress).toBe(
      (PROJECT_GUIDE_OUTPUT_LIMIT + 3) / 100,
    );
  });

  it("advances automated work into an explicit user checkpoint", () => {
    const { service, signals } = coordinator();
    reachCheckpoint(service, signals);

    expect(service.getSnapshot().value).toBe("checkpoint");
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(2);
    service.send({
      type: "USER_CHECKPOINT_COMPLETE",
      result: "Client connected",
    });

    expect(service.getSnapshot().value).toBe("waiting");
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(3);
    expect(
      service.getSnapshot().context.completedByPath["third-party-mcp"],
    ).toEqual([0, 1, 2]);
  });

  it("pauses and resumes the active operation through the adapter port", () => {
    const { service, signals } = coordinator();
    openMcp(service);
    service.send({ type: "START" });
    service.send({ type: "PAUSE" });

    expect(service.getSnapshot().value).toBe("paused");
    expect(signals.at(-1)).toEqual({
      type: "pause",
      scope: { path: "third-party-mcp", step: 0, attempt: 0, runId: 1 },
    });

    service.send({ type: "RESUME" });

    expect(service.getSnapshot().value).toBe("running");
    expect(signals.at(-1)).toEqual({
      type: "resume",
      scope: { path: "third-party-mcp", step: 0, attempt: 0, runId: 1 },
    });
  });

  it("waits for MCP baseline preparation before advancing", () => {
    const { service, signals } = coordinator();
    openMcp(service);
    service.send({ type: "START" });
    report(service, {
      type: "success",
      scope: latestScope(signals),
      result: "Server installed",
    });

    const { context } = service.getSnapshot();
    expect(service.getSnapshot().value).toBe("preparing");
    expect(getProjectGuideCurrentStep(context)).toBe(0);
    expect(context.completedByPath["third-party-mcp"]).toEqual([]);
    expect(signals.at(-1)).toEqual({
      type: "prepare",
      scope: { path: "third-party-mcp", step: 0, attempt: 0, runId: 1 },
    });

    report(service, {
      type: "success",
      scope: latestScope(signals),
      result: "Linear mcp server is now setup",
    });

    expect(service.getSnapshot().value).toBe("checkpoint");
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(1);
    expect(
      service.getSnapshot().context.completedByPath["third-party-mcp"],
    ).toEqual([0]);
  });

  it("starts Secret Step 2 immediately after an agent is selected", () => {
    const { service, signals } = coordinator();
    openSecret(service);
    service.send({ type: "START" });

    report(service, {
      type: "success",
      scope: latestScope(signals),
      result: "Secrets policy created",
    });

    expect(service.getSnapshot().value).toBe("checkpoint");
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(1);
    expect(service.getSnapshot().context.error).toBeNull();
    expect(signals).toHaveLength(1);

    service.send({ type: "SELECT_AGENT", client: "cursor" });

    expect(service.getSnapshot().value).toBe("running");
    expect(signals.at(-1)).toEqual({
      type: "start",
      scope: { path: "secret-block", step: 1, attempt: 0, runId: 2 },
    });

    report(service, {
      type: "success",
      scope: latestScope(signals),
      result: "Observability plugin downloaded",
    });

    expect(service.getSnapshot().value).toBe("checkpoint");
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(2);
  });

  it("retries an operation error without losing completed progress", () => {
    const { service, signals } = coordinator();
    openMcp(service);
    service.send({ type: "START" });
    report(service, {
      type: "error",
      scope: latestScope(signals),
      message: "Catalog unavailable",
    });

    expect(service.getSnapshot().value).toBe("error");
    expect(service.getSnapshot().context.error).toBe("Catalog unavailable");

    service.send({ type: "RETRY" });

    expect(service.getSnapshot().value).toBe("running");
    expect(service.getSnapshot().context.attempt).toBe(1);
    expect(signals.at(-1)).toEqual({
      type: "retry",
      scope: { path: "third-party-mcp", step: 0, attempt: 1, runId: 1 },
    });
  });

  it("tracks listening elapsed time and fails recoverably at the timeout", () => {
    const { service, signals } = coordinator();
    reachWaiting(service, signals);

    expect(service.getSnapshot().value).toBe("waiting");
    service.send({ type: "LISTEN_TICK", elapsedSeconds: 12 });
    expect(service.getSnapshot().context.elapsedListeningSeconds).toBe(12);

    service.send({
      type: "LISTEN_TICK",
      elapsedSeconds: LISTEN_TIMEOUT_SECONDS,
    });

    expect(service.getSnapshot().value).toBe("error");
    expect(service.getSnapshot().context.error).toContain("No event seen");
    expect(service.getSnapshot().context.output.at(-1)?.kind).toBe("error");
  });

  it("rewinds progress and aborts work at the prior step", () => {
    const { service, signals } = coordinator();
    reachCheckpoint(service, signals);
    service.send({ type: "REWIND", step: 1 });

    expect(service.getSnapshot().value).toBe("ready");
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(1);
    expect(
      service.getSnapshot().context.completedByPath["third-party-mcp"],
    ).toEqual([0]);
    expect(signals.at(-1)).toEqual({
      type: "abort",
      scope: { path: "third-party-mcp", step: 2, attempt: 0, runId: 1 },
      reason: "rewind",
    });
  });

  it("switches and backs out without stale active truth", () => {
    const { service, signals } = coordinator();
    openMcp(service);
    service.send({ type: "START" });
    service.send({ type: "SWITCH", path: "secret-block", resumeStep: 1 });

    expect(service.getSnapshot().value).toBe("checkpoint");
    expect(service.getSnapshot().context.activePath).toBe("secret-block");
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(1);
    expect(signals.at(-1)).toMatchObject({
      type: "abort",
      scope: { path: "third-party-mcp" },
      reason: "switch",
    });

    service.send({ type: "BACK" });
    expect(service.getSnapshot().value).toBe("opening");
    expect(service.getSnapshot().context.activePath).toBeNull();
  });

  it("completes only after a newly received event is recorded", () => {
    const { service, signals } = coordinator();
    reachWaiting(service, signals);

    report(service, {
      type: "event",
      scope: latestScope(signals),
      event: observedEvent,
    });

    const { context } = service.getSnapshot();
    expect(service.getSnapshot().value).toBe("complete");
    expect(context.observedEvent).toEqual(observedEvent);
    expect(context.completedByPath["third-party-mcp"]).toEqual([0, 1, 2, 3]);
    expect(context.output.at(-1)).toMatchObject({
      kind: "result",
      message: "Event received · tools/list",
    });
  });

  it("rejects a late success from a switched journey", () => {
    const { service, signals } = coordinator();
    openMcp(service);
    service.send({ type: "START" });
    const staleStart = signals.at(-1);
    expect(staleStart?.type).toBe("start");

    service.send({ type: "SWITCH", path: "secret-block", resumeStep: 0 });
    service.send({ type: "START" });
    const currentStart = signals.at(-1);
    expect(currentStart?.type).toBe("start");

    if (staleStart?.type !== "start" || currentStart?.type !== "start") {
      throw new Error("expected start signals");
    }
    service.send({
      type: "ADAPTER_REPORT",
      report: {
        type: "success",
        scope: staleStart.scope,
        result: "Late MCP success",
      },
    });

    expect(service.getSnapshot().context.activePath).toBe("secret-block");
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(0);

    service.send({
      type: "ADAPTER_REPORT",
      report: {
        type: "success",
        scope: currentStart.scope,
        result: "Current secret success",
      },
    });
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(1);
  });

  it("rejects a duplicate success from the attempt before retry", () => {
    const { service, signals } = coordinator();
    openMcp(service);
    service.send({ type: "START" });
    const firstAttempt = signals.at(-1);
    if (firstAttempt?.type !== "start") throw new Error("expected start");

    service.send({
      type: "ADAPTER_REPORT",
      report: {
        type: "error",
        scope: firstAttempt.scope,
        message: "Temporary failure",
      },
    });
    service.send({ type: "RETRY" });
    const retry = signals.at(-1);
    if (retry?.type !== "retry") throw new Error("expected retry");

    service.send({
      type: "ADAPTER_REPORT",
      report: {
        type: "success",
        scope: firstAttempt.scope,
        result: "Stale success",
      },
    });
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(0);

    service.send({
      type: "ADAPTER_REPORT",
      report: {
        type: "success",
        scope: retry.scope,
        result: "Retry success",
      },
    });
    const preparation = signals.at(-1);
    if (preparation?.type !== "prepare") {
      throw new Error("expected MCP preparation");
    }
    service.send({
      type: "ADAPTER_REPORT",
      report: {
        type: "success",
        scope: preparation.scope,
        result: "MCP ready",
      },
    });
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(1);
  });
});
