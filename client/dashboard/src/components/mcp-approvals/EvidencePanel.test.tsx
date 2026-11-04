import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { EvidencePanel } from "./EvidencePanel";
import { parseEvidenceDocument } from "./evidence";

afterEach(cleanup);
it.each([1, 2])(
  "does not infer publisher verification from omitted evidence (v%s)",
  (version) => {
    const document = parseEvidenceDocument(
      {
        identity: { kind: "remote" },
        provenance: {
          catalogued: true,
          registry: "native",
          specifier: "example/server",
        },
      },
      version,
    );
    expect(document).not.toBeNull();
    expect(document?.provenance).not.toHaveProperty("isLatest");
    render(
      <TooltipProvider>
        <EvidencePanel document={document} />
      </TooltipProvider>,
    );
    expect(screen.queryByText("Publisher vouched by the registry")).toBeNull();
    expect(screen.getByText("example/server")).toBeTruthy();
  },
);

it("keeps unsupported evidence versions unknown", () => {
  expect(parseEvidenceDocument({ identity: { kind: "remote" } }, 3)).toBeNull();
});

it.each([1, 2])(
  "ignores historical latest flags without changing facts (v%s)",
  (version) => {
    const base = {
      identity: { kind: "remote" },
      provenance: {
        catalogued: true,
        status: "active",
        updated_at: "2026-10-01T00:00:00Z",
      },
    };
    const expected = parseEvidenceDocument(base, version);
    for (const latest of [false, true]) {
      const historical = {
        ...base,
        provenance: { ...base.provenance, is_latest: latest },
      };
      expect(parseEvidenceDocument(historical, version)).toEqual(expected);
      expect(historical.provenance.is_latest).toBe(latest);
    }
  },
);
