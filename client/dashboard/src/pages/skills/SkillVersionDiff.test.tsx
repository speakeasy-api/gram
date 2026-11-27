import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import {
  cleanup,
  fireEvent,
  render as renderRaw,
  screen,
  within,
} from "@testing-library/react";
import { strToU8 } from "fflate";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { ReactNode } from "react";
import { SkillVersionDiff } from "./SkillVersionDiff";

function render(ui: ReactNode) {
  return renderRaw(<TooltipProvider>{ui}</TooltipProvider>);
}

const stored = vi.hoisted(
  () => new Map<string, { content: Uint8Array } | undefined>(),
);
const fetched = vi.hoisted(() => new Set<string>());

vi.mock("@gram/client/react-query/skillVersionFile.js", () => ({
  useSkillVersionFile: (
    request: { skillId: string; versionId: string; path: string },
    _security: unknown,
    options: { enabled?: boolean },
  ) => {
    if (options.enabled === false) return { isPending: true, error: null };
    const key = `${request.skillId}:${request.versionId}:${request.path}`;
    fetched.add(key);
    const data = stored.get(key);
    return { data, isPending: !data, error: null };
  },
}));

vi.mock("./SkillTextDiff", () => ({
  default: (props: {
    oldContent: string;
    newContent: string;
    newLabel: string;
    lang?: string;
  }) => (
    <pre data-testid="text-diff" data-lang={props.lang ?? "markdown"}>
      {props.newLabel}|{props.oldContent}→{props.newContent}
    </pre>
  ),
}));

afterEach(() => {
  cleanup();
  stored.clear();
  fetched.clear();
});

function file(
  path: string,
  sha256: string,
  contentType = "text/plain",
  sizeBytes = 10,
) {
  return { path, sha256, sizeBytes, contentType };
}

function version(
  id: string,
  content: string,
  files: ReturnType<typeof file>[],
): SkillVersion {
  return {
    id,
    content,
    canonicalSha256: `${id}00000000`,
    files,
  } as unknown as SkillVersion;
}

describe("SkillVersionDiff", () => {
  it("shows only the SKILL.md diff when neither version has files", async () => {
    render(
      <SkillVersionDiff
        skillId="skill_a"
        older={version("old", "a", [])}
        newer={version("new", "b", [])}
      />,
    );
    expect(screen.queryByRole("list", { name: "Changed files" })).toBeNull();
    expect((await screen.findByTestId("text-diff")).textContent).toContain(
      "a→b",
    );
  });

  it("lists added, removed, and changed files and diffs the selected one", async () => {
    stored.set("skill_a:old:scripts/run.py", { content: strToU8("print(1)") });
    stored.set("skill_a:new:scripts/run.py", { content: strToU8("print(2)") });
    render(
      <SkillVersionDiff
        skillId="skill_a"
        older={version("old", "same", [
          file("scripts/run.py", "1"),
          file("gone.md", "2"),
          file("kept.md", "3"),
        ])}
        newer={version("new", "same", [
          file("scripts/run.py", "9"),
          file("kept.md", "3"),
          file("assets/logo.png", "4", "image/png"),
        ])}
      />,
    );

    const list = screen.getByRole("list", { name: "Changed files" });
    const rows = within(list).getAllByRole("button");
    expect(rows.map((row) => row.textContent)).toEqual([
      expect.stringMatching(/^UnchangedSKILL\.md/),
      expect.stringMatching(/^Addedassets\/logo\.png/),
      expect.stringMatching(/^Removedgone\.md/),
      expect.stringMatching(/^Changedscripts\/run\.pyScript/),
    ]);
    expect(
      screen.getByText("1 unchanged supporting file not shown."),
    ).toBeTruthy();

    fireEvent.click(rows[3]!);
    const diff = await screen.findByTestId("text-diff");
    expect(diff.textContent).toContain("print(1)→print(2)");
    expect(diff.getAttribute("data-lang")).toBe("python");
  });

  it("explains that binary files have no line diff without fetching them", async () => {
    render(
      <SkillVersionDiff
        skillId="skill_a"
        older={version("old", "same", [])}
        newer={version("new", "same", [
          file("assets/logo.png", "4", "image/png"),
        ])}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /assets\/logo\.png/ }));
    expect(
      await screen.findByText(/is not a text file, so there is no line diff/),
    ).toBeTruthy();
    expect(fetched.size).toBe(0);
  });

  it("does not fetch text files too large to diff", async () => {
    render(
      <SkillVersionDiff
        skillId="skill_a"
        older={version("old", "same", [
          file("references/big.md", "1", "text/markdown", 10),
        ])}
        newer={version("new", "same", [
          file("references/big.md", "2", "text/markdown", 2 * 1024 * 1024),
        ])}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: /references\/big\.md/ }),
    );
    expect(await screen.findByText(/is too large to diff here/)).toBeTruthy();
    expect(fetched.size).toBe(0);
  });

  it("requests both sides of a diff scoped to the skill", async () => {
    stored.set("skill_a:old:notes.md", { content: strToU8("a") });
    stored.set("skill_a:new:notes.md", { content: strToU8("b") });
    render(
      <SkillVersionDiff
        skillId="skill_a"
        older={version("old", "same", [file("notes.md", "1")])}
        newer={version("new", "same", [file("notes.md", "2")])}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /notes\.md/ }));
    expect((await screen.findByTestId("text-diff")).textContent).toContain(
      "a→b",
    );
    expect([...fetched].sort()).toEqual([
      "skill_a:new:notes.md",
      "skill_a:old:notes.md",
    ]);
  });
});
