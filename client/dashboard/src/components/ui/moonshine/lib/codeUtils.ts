import { codeToTokens, BundledLanguage, BundledTheme } from "shiki";
import {
  ProgrammingLanguage,
  SupportedLanguage,
} from "@/components/ui/moonshine/types";

export const LIGHT_THEME = "github-light" as const;
export const DARK_THEME = "github-dark" as const;

interface CodeToken {
  content: string;
  color?: string;
  fontStyle?: number;
}

interface CodeLine {
  tokens: CodeToken[];
}

export interface HighlightedCode {
  lines: CodeLine[];
  code: string;
  lang: string;
}

/**
 * Highlights code using Shiki
 */
export async function highlightCode(
  code: string,
  language: SupportedLanguage | (string & {}),
  theme: BundledTheme = LIGHT_THEME,
): Promise<HighlightedCode> {
  // Clean the code by removing annotations
  const cleanCode = removeCodeHikeAnnotations(code);

  const lang = isProgrammingLanguage(language)
    ? getMappedLanguage(language)
    : (language as BundledLanguage);

  try {
    const tokens = await codeToTokens(cleanCode, {
      lang,
      theme,
    });

    const lines: CodeLine[] = tokens.tokens.map((line) => ({
      tokens: line.map((token) => ({
        content: token.content,
        color: token.color,
        fontStyle: token.fontStyle,
      })),
    }));

    return {
      lines,
      code: cleanCode,
      lang,
    };
  } catch (error) {
    console.error("Error highlighting code:", error);
    // Fallback to plain text
    return {
      lines: cleanCode.split("\n").map((line) => ({
        tokens: [{ content: line || "\n" }],
      })),
      code: cleanCode,
      lang,
    };
  }
}

/**
 * Maps language identifiers to their proper syntax highlighting aliases
 */
export function getMappedLanguage(
  language: ProgrammingLanguage | SupportedLanguage,
): BundledLanguage {
  switch (language) {
    case "javascript":
      return "js";
    case "typescript":
      return "ts";
    case "python":
      return "py";
    case "bash":
      return "bash";
    case "json":
      return "json";
    case "go":
      return "go";
    case "dotnet":
    case "csharp":
      return "csharp";
    case "java":
      return "java";
    case "ruby":
      return "ruby";
    case "php":
      return "php";
    case "swift":
      return "swift";
    case "terraform":
      return "hcl";
    case "postman":
      return language as BundledLanguage;
    case "unity":
      return language as BundledLanguage;
    default:
      return language as BundledLanguage;
  }
}

/**
 * Helper to check if a language is in our supported set
 */
export function isProgrammingLanguage(
  language: string,
): language is ProgrammingLanguage {
  return [
    "javascript",
    "typescript",
    "python",
    "bash",
    "json",
    "go",
    "dotnet",
    "csharp",
    "java",
    "ruby",
    "php",
    "swift",
    "terraform",
  ].includes(language);
}

const ANNOTATION_TYPES = [
  "callout",
  "className",
  "hover",
  "collapse",
  "diff",
  "focus",
  "fold",
  "link",
  "mark",
  "tooltip",
];

const ANNOTATION_REGEX = new RegExp(
  `^\\s*#\\s*!(${ANNOTATION_TYPES.join("|")})(\\s*$begin:math:text$[^)]*\\$end:math:text$)?.*$`,
);

/**
 * Removes CodeHike annotations from the code
 * Useful for copying code / excluding annotations from clipboard
 * @param code - The code string containing CodeHike annotations
 * @returns Clean code string without annotations
 */
export function removeCodeHikeAnnotations(code: string): string {
  const lines = code.split("\n");
  const result: string[] = [];
  let skipEmpty = false;

  for (const line of lines) {
    if (ANNOTATION_REGEX.test(line)) {
      skipEmpty = true;
      continue;
    }

    if (line.trim() === "" && skipEmpty) {
      skipEmpty = false;
      continue;
    }

    result.push(line);
    skipEmpty = false;
  }

  return result.join("\n");
}
