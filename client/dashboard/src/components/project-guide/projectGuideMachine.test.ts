import { createActor } from "xstate";
import { describe, expect, it } from "vitest";
import { projectGuideMachine } from "./projectGuideMachine";

function actor() {
  const service = createActor(projectGuideMachine, {
    input: { initialStep: 0, completed: [] },
  });
  service.start();
  return service;
}

describe("projectGuideMachine", () => {
  it("requires the pick gate before running", () => {
    const service = actor();
    service.send({ type: "START" });
    expect(service.getSnapshot().value).toBe("await");

    service.send({ type: "SELECT", value: "Linear" });
    expect(service.getSnapshot().value).toBe("running");
    service.stop();
  });

  it("supports pause and resume without losing the current step", () => {
    const service = actor();
    service.send({ type: "START" });
    service.send({ type: "SELECT", value: "Linear" });
    service.send({ type: "PAUSE" });
    expect(service.getSnapshot().value).toBe("paused");
    service.send({ type: "RESUME" });
    expect(service.getSnapshot().value).toBe("running");
    expect(service.getSnapshot().context.step).toBe(0);
    service.stop();
  });

  it("caps retry logs and records a listened event", () => {
    const service = actor();
    service.send({ type: "START" });
    service.send({ type: "SELECT", value: "Linear" });
    service.send({ type: "FAIL", message: "temporary" });
    expect(service.getSnapshot().value).toBe("error");
    service.send({ type: "RETRY" });
    expect(service.getSnapshot().context.attempt).toBe(1);
    service.send({
      type: "EVENT_RECEIVED",
      event: {
        kind: "Governed call",
        tone: "allow",
        title: "tools/list",
        rows: [{ key: "actor", value: "agent" }],
        note: "kept on the record",
      },
    });
    expect(service.getSnapshot().value).toBe("success");
    expect(service.getSnapshot().context.event?.title).toBe("tools/list");
    service.stop();
  });

  it("rewinds completed progress and clears copy gates", () => {
    const service = createActor(projectGuideMachine, {
      input: { initialStep: 2, completed: [0, 1] },
    }).start();
    service.send({ type: "REWIND", step: 1 });
    expect(service.getSnapshot().context.step).toBe(1);
    expect(service.getSnapshot().context.completed).toEqual([0]);
    expect(service.getSnapshot().context.copied).toBe(false);
    service.stop();
  });
});
