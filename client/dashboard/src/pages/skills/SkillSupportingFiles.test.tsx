import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { SkillResourceReference } from "@gram/client/models/components/skillresourcereference.js";

import { SkillSupportingFiles } from "./SkillSupportingFiles";

function renderSection(references: SkillResourceReference[]) {
  return render(
    <TooltipProvider>
      <SkillSupportingFiles references={references} />
    </TooltipProvider>,
  );
}

afterEach(cleanup);

describe("SkillSupportingFiles", () => {
  it("renders nothing when the manifest references no supporting files", () => {
    const { container } = renderSection([]);

    expect(container.innerHTML).toBe("");
  });

  it("lists each referenced file with its kind", () => {
    renderSection([
      { path: "assets/template.docx", kind: "asset" },
      { path: "references/REFERENCE.md", kind: "reference" },
    ]);

    expect(screen.getByText("assets/template.docx")).toBeTruthy();
    expect(screen.getByText("references/REFERENCE.md")).toBeTruthy();
    expect(screen.getByText("Asset")).toBeTruthy();
    expect(screen.getByText("Reference")).toBeTruthy();
  });

  it("explains that supporting files are neither stored nor distributed", () => {
    renderSection([{ path: "references/REFERENCE.md", kind: "reference" }]);

    expect(
      screen.getByText(/not included when the skill is distributed/),
    ).toBeTruthy();
    expect(screen.queryByText(/executable content/)).toBeNull();
  });

  it("calls out executable content when the manifest references a script", () => {
    renderSection([{ path: "scripts/extract.py", kind: "script" }]);

    expect(screen.getByText("Script")).toBeTruthy();
    expect(
      screen.getByText(/runs executable content Gram cannot show you/),
    ).toBeTruthy();
  });
});
