import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  cleanup,
  fireEvent,
  render as renderRaw,
  screen,
} from "@testing-library/react";
import { strToU8 } from "fflate";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SkillFileList, SkillFileViewer } from "./SkillFiles";

const testState = vi.hoisted(() => ({
  data: undefined as { content: Uint8Array } | undefined,
}));

vi.mock("@gram/client/react-query/skillVersionFile.js", () => ({
  useSkillVersionFile: () => ({
    data: testState.data,
    isPending: !testState.data,
    error: null,
  }),
}));
vi.mock("@/elements/components/Markdown", () => ({
  Markdown: ({ children }: { children: string }) => (
    <div data-testid="markdown">{children}</div>
  ),
}));
vi.mock("@/components/code", () => ({
  CodeBlock: ({
    children,
    language,
  }: {
    children: string;
    language: string;
  }) => (
    <pre data-testid="code" data-language={language}>
      {children}
    </pre>
  ),
}));

function render(ui: ReactNode) {
  return renderRaw(<TooltipProvider>{ui}</TooltipProvider>);
}

afterEach(() => {
  cleanup();
  testState.data = undefined;
});

const file = (path: string, contentType: string) => ({
  path,
  contentType,
  sha256: "a".repeat(64),
  sizeBytes: 2048,
});

describe("SkillFileList", () => {
  it("lists SKILL.md first, marks scripts, and reports selection", () => {
    const onSelect = vi.fn<(path: string) => void>();
    render(
      <SkillFileList
        files={[
          file("references/REFERENCE.md", "text/markdown"),
          file("scripts/run.sh", "text/x-shellscript"),
        ]}
        manifestSizeBytes={300}
        selectedPath="SKILL.md"
        onSelect={onSelect}
      />,
    );
    const rows = screen.getAllByRole("button");
    expect(rows.map((row) => row.textContent)).toEqual([
      "SKILL.md300 B",
      "references/REFERENCE.md2.0 KB",
      "scripts/run.shScript2.0 KB",
    ]);
    expect(rows[0]?.getAttribute("aria-current")).toBe("true");
    fireEvent.click(rows[2]!);
    expect(onSelect).toHaveBeenCalledWith("scripts/run.sh");
  });
});

describe("SkillFileViewer", () => {
  it("renders Markdown files as Markdown", () => {
    testState.data = { content: strToU8("# Reference") };
    render(
      <SkillFileViewer
        skillId="skill_a"
        versionId="version_a"
        file={file("references/REFERENCE.md", "text/markdown; charset=utf-8")}
      />,
    );
    expect(screen.getByTestId("markdown").textContent).toBe("# Reference");
    expect(screen.getByText(/text\/markdown · 2\.0 KB/)).toBeTruthy();
  });

  it("renders code with highlighting and flags scripts", () => {
    testState.data = { content: strToU8("print(1)") };
    render(
      <SkillFileViewer
        skillId="skill_a"
        versionId="version_a"
        file={file("scripts/run.py", "text/x-python; charset=utf-8")}
      />,
    );
    const code = screen.getByTestId("code");
    expect(code.textContent).toBe("print(1)");
    expect(code.getAttribute("data-language")).toBe("python");
    expect(screen.getByText(/agents can run this file/)).toBeTruthy();
  });

  it("shows SVG source instead of rendering it, whatever the extension case", () => {
    testState.data = { content: strToU8("<svg></svg>") };
    render(
      <SkillFileViewer
        skillId="skill_a"
        versionId="version_a"
        file={file("assets/LOGO.SVG", "image/svg+xml")}
      />,
    );
    expect(screen.queryByRole("img")).toBeNull();
    const code = screen.getByTestId("code");
    expect(code.textContent).toBe("<svg></svg>");
    expect(code.getAttribute("data-language")).toBe("xml");
  });

  it("offers a download for binary files", () => {
    testState.data = { content: new Uint8Array([0, 1, 2, 3]) };
    render(
      <SkillFileViewer
        skillId="skill_a"
        versionId="version_a"
        file={file("assets/data.bin", "application/octet-stream")}
      />,
    );
    expect(screen.getByText("No preview for this file type")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Download" }).hasAttribute("disabled"),
    ).toBe(false);
  });

  it("disables download while the file loads", () => {
    render(
      <SkillFileViewer
        skillId="skill_a"
        versionId="version_a"
        file={file("assets/data.bin", "application/octet-stream")}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Download" }).hasAttribute("disabled"),
    ).toBe(true);
  });
});
