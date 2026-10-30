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
    expect(document?.provenance?.isLatest).toBeUndefined();
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
