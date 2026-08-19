import { createActor } from "xstate";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  PROJECT_GUIDE_OUTPUT_LIMIT,
  getProjectGuideCurrentStep,
  projectGuideMachine,
  type ProjectGuideEventCard,
  type ProjectGuideOperationSignal,
} from "./projectGuideMachine";

function coordinator(listenTimeoutSeconds = 60) {
  const signals: ProjectGuideOperationSignal[] = [];
  const service = createActor(projectGuideMachine, {
    input: {
      listenTimeoutSeconds,
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

function reachCheckpoint(
  service: ReturnType<typeof coordinator>["service"],
): void {
  openMcp(service);
  service.send({ type: "START" });
  service.send({ type: "OPERATION_SUCCESS", result: "Server installed" });
  service.send({ type: "OPERATION_SUCCESS", result: "Endpoint verified" });
}

function reachWaiting(
  service: ReturnType<typeof coordinator>["service"],
): void {
  reachCheckpoint(service);
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
      { type: "start", path: "third-party-mcp", step: 0 },
    ]);
    expect(service.getSnapshot().context.output.at(-1)).toMatchObject({
      kind: "start",
      message: "Started · Pick a server from the catalog",
    });
  });

  it("records automated progress and caps visible output history", () => {
    const { service } = coordinator();
    openMcp(service);
    service.send({ type: "START" });

    for (let index = 0; index < PROJECT_GUIDE_OUTPUT_LIMIT + 4; index++) {
      service.send({
        type: "OPERATION_PROGRESS",
        message: `progress ${index}`,
        progress: index / 100,
      });
    }

    const { context } = service.getSnapshot();
    expect(context.output).toHaveLength(PROJECT_GUIDE_OUTPUT_LIMIT);
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
    reachCheckpoint(service);

    expect(service.getSnapshot().value).toBe("checkpoint");
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(2);
    expect(service.getSnapshot().context.checkpoint).toEqual({
      step: 2,
      label: "Connect your client",
    });
    expect(signals).toContainEqual({
      type: "start",
      path: "third-party-mcp",
      step: 1,
    });

    service.send({
      type: "USER_CHECKPOINT_COMPLETE",
      result: "Client connected",
    });

    expect(service.getSnapshot().value).toBe("checkpoint");
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
      path: "third-party-mcp",
      step: 0,
    });

    service.send({ type: "RESUME" });

    expect(service.getSnapshot().value).toBe("running");
    expect(signals.at(-1)).toEqual({
      type: "resume",
      path: "third-party-mcp",
      step: 0,
    });
  });

  it("records success and advances to the next operation", () => {
    const { service, signals } = coordinator();
    openMcp(service);
    service.send({ type: "START" });
    service.send({ type: "OPERATION_SUCCESS", result: "Server installed" });

    const { context } = service.getSnapshot();
    expect(service.getSnapshot().value).toBe("running");
    expect(getProjectGuideCurrentStep(context)).toBe(1);
    expect(context.completedByPath["third-party-mcp"]).toEqual([0]);
    expect(context.output.slice(-2).map((entry) => entry.kind)).toEqual([
      "result",
      "next",
    ]);
    expect(signals.at(-1)).toEqual({
      type: "start",
      path: "third-party-mcp",
      step: 1,
    });
  });

  it("retries an operation error without losing completed progress", () => {
    const { service, signals } = coordinator();
    openMcp(service);
    service.send({ type: "START" });
    service.send({ type: "OPERATION_ERROR", message: "Catalog unavailable" });

    expect(service.getSnapshot().value).toBe("error");
    expect(service.getSnapshot().context.error).toBe("Catalog unavailable");

    service.send({ type: "RETRY" });

    expect(service.getSnapshot().value).toBe("running");
    expect(service.getSnapshot().context.attempt).toBe(1);
    expect(signals.at(-1)).toEqual({
      type: "retry",
      path: "third-party-mcp",
      step: 0,
      attempt: 1,
    });
  });

  it("tracks listening elapsed time and fails recoverably at the timeout", () => {
    const { service } = coordinator(60);
    reachWaiting(service);

    expect(service.getSnapshot().value).toBe("waiting");
    service.send({ type: "LISTEN_TICK", elapsedSeconds: 12 });
    expect(service.getSnapshot().context.elapsedListeningSeconds).toBe(12);

    service.send({ type: "LISTEN_TICK", elapsedSeconds: 60 });

    expect(service.getSnapshot().value).toBe("error");
    expect(service.getSnapshot().context.error).toContain("No event seen");
  });

  it("rewinds progress and aborts work at the prior step", () => {
    const { service, signals } = coordinator();
    reachCheckpoint(service);
    service.send({ type: "REWIND", step: 1 });

    expect(service.getSnapshot().value).toBe("ready");
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(1);
    expect(
      service.getSnapshot().context.completedByPath["third-party-mcp"],
    ).toEqual([0]);
    expect(signals.at(-1)).toEqual({
      type: "abort",
      path: "third-party-mcp",
      step: 2,
      reason: "rewind",
    });
  });

  it("switches, backs out, exits, and resets without stale active truth", () => {
    const { service, signals } = coordinator();
    openMcp(service);
    service.send({ type: "START" });
    service.send({ type: "SWITCH", path: "secret-block", resumeStep: 1 });

    expect(service.getSnapshot().value).toBe("ready");
    expect(service.getSnapshot().context.activePath).toBe("secret-block");
    expect(getProjectGuideCurrentStep(service.getSnapshot().context)).toBe(1);
    expect(signals.at(-1)).toMatchObject({
      type: "abort",
      path: "third-party-mcp",
      reason: "switch",
    });

    service.send({ type: "BACK" });
    expect(service.getSnapshot().value).toBe("opening");
    expect(service.getSnapshot().context.activePath).toBeNull();

    service.send({ type: "OPEN", path: "secret-block", resumeStep: 1 });
    service.send({ type: "EXIT" });
    expect(service.getSnapshot().value).toBe("exited");
    expect(signals.at(-1)).toMatchObject({
      type: "abort",
      path: "secret-block",
      reason: "exit",
    });

    service.send({ type: "RESET" });
    expect(service.getSnapshot().value).toBe("opening");
    expect(service.getSnapshot().context.completedByPath).toEqual({
      "third-party-mcp": [],
      "secret-block": [],
    });
  });

  it("completes only after a newly received event is recorded", () => {
    const { service } = coordinator();
    reachWaiting(service);

    service.send({ type: "EVENT_RECEIVED", event: observedEvent });

    const { context } = service.getSnapshot();
    expect(service.getSnapshot().value).toBe("complete");
    expect(context.observedEvent).toEqual(observedEvent);
    expect(context.completedByPath["third-party-mcp"]).toEqual([0, 1, 2, 3, 4]);
    expect(context.output.at(-1)).toMatchObject({
      kind: "result",
      message: "Event received · tools/list",
    });
  });
});
