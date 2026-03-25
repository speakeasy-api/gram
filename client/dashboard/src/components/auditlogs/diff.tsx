import { AuditLog } from "@gram/client/models/components";
import { FileDiffOptions, ThemeTypes } from "@pierre/diffs";
import { MultiFileDiff } from "@pierre/diffs/react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import React from "react";

const staticDiffOptions: FileDiffOptions<undefined> = {
  theme: { dark: "pierre-dark", light: "pierre-light" },
  themeType: "system",
  diffStyle: "split",
  disableFileHeader: true,
  disableLineNumbers: true,
};

function prepareSnapshot(
  action: AuditLog["action"],
  snapshot: unknown,
): string {
  if (snapshot == null) {
    return "";
  }

  switch (action) {
    case "toolset:update": {
      const cloned = JSON.parse(JSON.stringify(snapshot));
      delete cloned.UpdatedAt;

      if (Array.isArray(cloned.SecurityVariables)) {
        cloned.SecurityVariables.sort(
          (a: { Name: string }, b: { Name: string }) =>
            a.Name.localeCompare(b.Name),
        );
      }

      return JSON.stringify(cloned, null, 2);
    }
    default:
      return JSON.stringify(snapshot, null, 2);
  }
}

export function StaticDiff(props: { log: AuditLog; lang?: string }) {
  const { theme } = useMoonshineConfig();

  const { log, lang } = props;
  const { oldFile, newFile } = React.useMemo(
    () => ({
      oldFile: {
        name: "before",
        contents: prepareSnapshot(log.action, log.beforeSnapshot),
        lang,
      },
      newFile: {
        name: "after",
        contents: prepareSnapshot(log.action, log.afterSnapshot),
        lang,
      },
    }),
    [log.action, log.beforeSnapshot, log.afterSnapshot, lang],
  );

  let themeType: ThemeTypes = "system";
  if (theme === "light") {
    themeType = "light";
  } else if (theme === "dark") {
    themeType = "dark";
  } else {
    themeType = "system";
  }

  return (
    <MultiFileDiff
      oldFile={oldFile}
      newFile={newFile}
      options={{ ...staticDiffOptions, themeType }}
    />
  );
}
