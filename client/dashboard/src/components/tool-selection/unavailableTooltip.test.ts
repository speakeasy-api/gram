import { describe, expect, it } from "vitest";
import { unavailableTooltip } from "./unavailableTooltip";

describe("unavailableTooltip", () => {
  it("explains an unproxied server shown with the default label", () => {
    expect(unavailableTooltip({})).toMatch(/doesn't proxy this server/);
  });

  it("adds nothing to a row with its own label and no tooltip", () => {
    expect(
      unavailableTooltip({
        unavailableLabel: "Tools are resolved when the server is called",
      }),
    ).toBeUndefined();
  });

  it("keeps a caller's own tooltip", () => {
    expect(
      unavailableTooltip({
        unavailableLabel: "custom",
        unavailableTooltip: "why",
      }),
    ).toBe("why");
  });
});
