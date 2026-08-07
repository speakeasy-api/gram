import { HighlightProvider } from "@/components/diffs/provider";
import { useIsMobile } from "@/hooks/use-mobile";
import type {
  DiffLineAnnotation,
  FileDiffOptions,
  ThemeTypes,
} from "@pierre/diffs";
import { MultiFileDiff } from "@pierre/diffs/react";
import { useConfig as useMoonshineConfig } from "@/components/ui/hooks/useConfig";
import type { ReactNode } from "react";

export type SkillTextDiffProps<Annotation> = {
  oldContent: string;
  newContent: string;
  oldLabel: string;
  newLabel: string;
  lineAnnotations?: DiffLineAnnotation<Annotation>[];
  renderAnnotation?: (annotation: DiffLineAnnotation<Annotation>) => ReactNode;
};

export default function SkillTextDiff<Annotation = undefined>({
  oldContent,
  newContent,
  oldLabel,
  newLabel,
  lineAnnotations,
  renderAnnotation,
}: SkillTextDiffProps<Annotation>): JSX.Element {
  const isMobile = useIsMobile();
  const { theme } = useMoonshineConfig();
  let themeType: ThemeTypes = "system";
  if (theme === "light") themeType = "light";
  if (theme === "dark") themeType = "dark";

  const options: FileDiffOptions<Annotation> = {
    theme: { dark: "pierre-dark", light: "pierre-light" },
    themeType,
    // Review comments need the full width of a line, so annotated diffs stay
    // unified rather than splitting into two narrow columns.
    diffStyle: isMobile || lineAnnotations != null ? "unified" : "split",
    // A manifest is prose, so wrap long lines rather than scrolling sideways.
    // Wrapping also sizes annotations to the visible column instead of the
    // widest line, which keeps a review comment fully readable.
    overflow: "wrap",
    disableFileHeader: false,
    disableLineNumbers: false,
  };

  return (
    <HighlightProvider langs={["markdown"]}>
      {/* The diff paints its own square-cornered surface, so it is clipped to
          the rounded border rather than allowed to cut through it. */}
      <div className="overflow-hidden border">
        <MultiFileDiff
          oldFile={{ name: oldLabel, contents: oldContent, lang: "markdown" }}
          newFile={{ name: newLabel, contents: newContent, lang: "markdown" }}
          options={options}
          lineAnnotations={lineAnnotations}
          renderAnnotation={renderAnnotation}
        />
      </div>
    </HighlightProvider>
  );
}
