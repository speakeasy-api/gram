import { describe, expect, it } from "vitest";
import { ACTIVE_AGENT_PROVIDER_IDS } from "@/components/agent-providers/agent-providers";
import { AGENT_PLATFORMS } from "./setup-data";

describe("AGENT_PLATFORMS", () => {
  it("follows the shared setup provider order", () => {
    expect(
      AGENT_PLATFORMS.slice(0, ACTIVE_AGENT_PROVIDER_IDS.setup.length).map(
        ({ id }) => id,
      ),
    ).toEqual([...ACTIVE_AGENT_PROVIDER_IDS.setup]);
  });
});
