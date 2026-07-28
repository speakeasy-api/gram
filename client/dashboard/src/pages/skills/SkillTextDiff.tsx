import { HighlightProvider } from "@/components/diffs/provider";
import { useIsMobile } from "@/hooks/use-mobile";
import type { FileDiffOptions, ThemeTypes } from "@pierre/diffs";
import { MultiFileDiff } from "@pierre/diffs/react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";

export default function SkillTextDiff({
  oldContent,
  newContent,
  oldLabel,
  newLabel,
}: {
  oldContent: string;
  newContent: string;
  oldLabel: string;
  newLabel: string;
}): JSX.Element {
  const isMobile = useIsMobile();
  const { theme } = useMoonshineConfig();
  let themeType: ThemeTypes = "system";
  if (theme === "light") themeType = "light";
  if (theme === "dark") themeType = "dark";

  const options: FileDiffOptions<undefined> = {
    theme: { dark: "pierre-dark", light: "pierre-light" },
    themeType,
    diffStyle: isMobile ? "unified" : "split",
    disableFileHeader: false,
    disableLineNumbers: false,
  };

  return (
    <HighlightProvider langs={["markdown"]}>
      <div className="overflow-x-auto rounded-lg border">
        <MultiFileDiff
          oldFile={{ name: oldLabel, contents: oldContent, lang: "markdown" }}
          newFile={{ name: newLabel, contents: newContent, lang: "markdown" }}
          options={options}
        />
      </div>
    </HighlightProvider>
  );
}
