import { AgentPolicyGrantEffect$inboundSchema } from "@gram/client/models/components/agentpolicygrant.js";
import { KillswitchSchedule$inboundSchema } from "@gram/client/models/components/killswitchschedule.js";
import { KillswitchScope$inboundSchema } from "@gram/client/models/components/killswitchscope.js";
import { NotApplicableReason$inboundSchema } from "@gram/client/models/components/oktaresourceconnectionserver.js";
import { Engine$inboundSchema } from "@gram/client/models/components/prompttemplate.js";
import { UpdatePromptTemplateFormEngine$outboundSchema } from "@gram/client/models/components/updateprompttemplateform.js";
import { describe, expect, it } from "vitest";

describe("generated singleton enum compatibility", () => {
  it.each([
    ["Okta connection reason", NotApplicableReason$inboundSchema],
    ["agent policy effect", AgentPolicyGrantEffect$inboundSchema],
    ["prompt engine", Engine$inboundSchema],
  ])("preserves unknown %s response values", (_, schema) => {
    expect(schema.parse("future_value")).toBe("future_value");
    expect(schema.safeParse(42).success).toBe(false);
  });

  it("keeps request-only prompt engines closed", () => {
    expect(
      UpdatePromptTemplateFormEngine$outboundSchema.parse("mustache"),
    ).toBe("mustache");
    expect(
      UpdatePromptTemplateFormEngine$outboundSchema.safeParse("future_value")
        .success,
    ).toBe(false);
  });

  it.each([
    { start: "scheduled", end: "bounded" },
    { start: "scheduled", end: "until_lifted" },
    { start: "now", end: "bounded" },
    { start: "future_start", end: "until_lifted" },
    { start: "now", end: "future_end" },
  ])(
    "rejects invalid schedule %j instead of selecting a date-free branch",
    (schedule) => {
      expect(KillswitchSchedule$inboundSchema.safeParse(schedule).success).toBe(
        false,
      );
    },
  );

  it("retains required schedule dates for the matching variant", () => {
    const start = "2026-01-01T00:00:00Z";
    const end = "2026-01-02T00:00:00Z";
    expect(
      KillswitchSchedule$inboundSchema.parse({
        start: "scheduled",
        starts_at: start,
        end: "bounded",
        ends_at: end,
      }),
    ).toEqual({
      start: "scheduled",
      startsAt: new Date(start),
      end: "bounded",
      endsAt: new Date(end),
    });
  });

  it("keeps structural union discriminators closed", () => {
    expect(
      KillswitchScope$inboundSchema.safeParse({ type: "future_scope" }).success,
    ).toBe(false);
  });
});
