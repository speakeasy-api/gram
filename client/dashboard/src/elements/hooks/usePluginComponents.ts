import type {
  CodeHeaderProps,
  SyntaxHighlighterProps,
} from "@assistant-ui/react-markdown";
import type { ComponentType } from "react";
import { useMemo } from "react";
import type { Plugin } from "@/elements/types/plugins";

type ComponentsByLanguage =
  | Record<
      string,
      {
        CodeHeader?: ComponentType<CodeHeaderProps> | undefined;
        SyntaxHighlighter?: ComponentType<SyntaxHighlighterProps> | undefined;
      }
    >
  | undefined;

export function useComponentsByLanguage(
  plugins: Plugin[],
): ComponentsByLanguage {
  return useMemo(() => {
    return plugins.reduce((acc, plugin) => {
      if (acc?.[plugin.language] && !plugin.overrideExisting) {
        return acc;
      }
      acc = {
        ...acc,
        [plugin.language]: {
          CodeHeader: plugin.Header ?? (() => null),
          SyntaxHighlighter: plugin.Component ?? undefined,
        },
      };
      return acc;
    }, {} as ComponentsByLanguage);
  }, [plugins]);
}
