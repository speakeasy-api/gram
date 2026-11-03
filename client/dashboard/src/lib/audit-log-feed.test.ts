import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import { describe, expect, it } from "vitest";
import { groupConsecutiveByActor } from "./audit-log-feed";

function log(id: string, actorId: string, actorType = "agent"): AuditLog {
  return {
    id,
    actorId,
    actorType,
    action: "workload-admission:admit",
    subjectId: id,
    subjectType: "workload_admission",
    actingSurface: "system",
    category: "security",
    createdAt: new Date(),
  } as AuditLog;
}

describe("groupConsecutiveByActor", () => {
  it("groups consecutive entries by the same actor and keeps order", () => {
    const runs = groupConsecutiveByActor([
      log("1", "a"),
      log("2", "a"),
      log("3", "b"),
      log("4", "a"),
    ]);
    expect(runs.map((run) => run.logs.map((l) => l.id))).toEqual([
      ["1", "2"],
      ["3"],
      ["4"],
    ]);
    expect(runs[0]!.key).toBe("1");
  });

  it("does not merge the same id under different actor types", () => {
    const runs = groupConsecutiveByActor([
      log("1", "x", "agent"),
      log("2", "x", "user"),
    ]);
    expect(runs).toHaveLength(2);
  });

  it("returns nothing for no entries", () => {
    expect(groupConsecutiveByActor([])).toEqual([]);
  });
});
