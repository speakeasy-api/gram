import { describe, expect, it } from "vitest";
import { admissionValuesDiffer, changedAdmissionFields } from "./admissionEdit";

const stored = {
  subject: "wimse://identity.example.com/org/acme/agent/a-1",
  name: "Deploy bot",
  tags: ["production", "ci"],
  agentId: "22222222-2222-2222-2222-222222222222",
};

describe("changedAdmissionFields", () => {
  it("is empty when nothing changed", () => {
    expect(changedAdmissionFields(stored, { ...stored })).toEqual({});
    expect(admissionValuesDiffer(stored, { ...stored })).toBe(false);
  });

  it("differs when the label, tags or agent change", () => {
    expect(
      admissionValuesDiffer(stored, { ...stored, name: "Release bot" }),
    ).toBe(true);
    expect(admissionValuesDiffer(stored, { ...stored, tags: ["ci"] })).toBe(
      true,
    );
    expect(
      admissionValuesDiffer(stored, {
        ...stored,
        agentId: "33333333-3333-3333-3333-333333333333",
      }),
    ).toBe(true);
  });

  it("ignores surrounding whitespace on the label, as the server trims it", () => {
    expect(
      changedAdmissionFields(stored, { ...stored, name: "  Deploy bot " }),
    ).toEqual({});
  });

  it("includes each changed field, the label trimmed", () => {
    expect(
      changedAdmissionFields(stored, {
        ...stored,
        name: " Release bot ",
        tags: ["ci", "production"],
        agentId: "33333333-3333-3333-3333-333333333333",
      }),
    ).toEqual({
      name: "Release bot",
      tags: ["ci", "production"],
      agentId: "33333333-3333-3333-3333-333333333333",
    });
  });

  it("sends a cleared label and cleared tags", () => {
    expect(
      changedAdmissionFields(stored, { ...stored, name: "  ", tags: [] }),
    ).toEqual({ name: "", tags: [] });
  });

  it("never includes the subject", () => {
    expect(
      changedAdmissionFields(stored, {
        ...stored,
        subject: "wimse://identity.example.com/org/acme/agent/*",
      }),
    ).toEqual({});
  });

  it("never sends an empty agent", () => {
    expect(changedAdmissionFields(stored, { ...stored, agentId: "" })).toEqual(
      {},
    );
  });
});
