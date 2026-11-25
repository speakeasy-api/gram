import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { MdmRolloutTable } from "./mdm-rollout-table";
import { MDM_TARGETS, mdmGuideUrl } from "./mdm-targets";

afterEach(cleanup);

describe("MdmRolloutTable", () => {
  it("links every target to its guide in a new tab", () => {
    render(<MdmRolloutTable />);

    const links = screen.getAllByRole("link", { name: /Open guide/ });
    expect(links).toHaveLength(MDM_TARGETS.length);
    for (const [i, link] of links.entries()) {
      expect(link.getAttribute("href")).toBe(mdmGuideUrl(MDM_TARGETS[i]!));
      expect(link.getAttribute("target")).toBe("_blank");
      expect(link.getAttribute("rel")).toContain("noopener");
    }
    expect(mdmGuideUrl({ id: "jamf" })).toBe(
      "https://www.speakeasy.com/docs/ai-control-plane/reference/device-agent/mdm-installations/jamf",
    );
  });
});
