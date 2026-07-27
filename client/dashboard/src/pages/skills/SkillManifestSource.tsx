import { HighlightProvider } from "@/components/diffs/provider";
import type { FileOptions, LineAnnotation, ThemeTypes } from "@pierre/diffs";
import { File } from "@pierre/diffs/react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import type { ReactNode } from "react";
import type { SkillDiffAnchor } from "./skill-diff-anchors";

/**
 * Renders SKILL.md exactly as agents load it, with review markers pinned to the
 * lines a suggestion touches. Line numbers must match the stored manifest, so
 * this renders the raw source including frontmatter.
 */
export default function SkillManifestSource({
  content,
  anchors,
  renderAnchor,
}: {
  content: string;
  anchors: SkillDiffAnchor[];
  renderAnchor: (anchor: SkillDiffAnchor) => ReactNode;
}): JSX.Element {
  const { theme } = useMoonshineConfig();
  let themeType: ThemeTypes = "system";
  if (theme === "light") themeType = "light";
  if (theme === "dark") themeType = "dark";

  const options: FileOptions<SkillDiffAnchor> = {
    theme: { dark: "pierre-dark", light: "pierre-light" },
    themeType,
    disableFileHeader: true,
    disableLineNumbers: false,
    overflow: "wrap",
  };

  const lineAnnotations: LineAnnotation<SkillDiffAnchor>[] = anchors.map(
    (anchor) => ({ lineNumber: anchor.line, metadata: anchor }),
  );

  return (
    <HighlightProvider langs={["markdown"]}>
      <div className="overflow-x-auto rounded-lg border">
        <File
          file={{ name: "SKILL.md", contents: content, lang: "markdown" }}
          options={options}
          lineAnnotations={lineAnnotations}
          renderAnnotation={(annotation) => renderAnchor(annotation.metadata)}
        />
      </div>
    </HighlightProvider>
  );
}
