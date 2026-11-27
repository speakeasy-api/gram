import { CodeBlock } from "@/components/code";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Markdown } from "@/elements/components/Markdown";
import { cn } from "@/lib/utils";
import type { SkillFile } from "@gram/client/models/components/skillfile.js";
import { useSkillVersionFile } from "@gram/client/react-query/skillVersionFile.js";
import { Download } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  bytesFromContent,
  decodeTextContent,
  describeContentType,
  downloadSkillFile,
  formatFileSize,
  isLikelyTextFile,
  isMarkdownFile,
  isSkillScript,
  languageForPath,
  SKILL_MANIFEST_PATH,
} from "./skill-files";

// Highlighting a multi-megabyte file freezes the tab; larger text files are
// offered as a download instead.
export const MAX_PREVIEW_BYTES = 512 * 1024;

const SCRIPT_HINT =
  "Agents can run files in scripts/. Review them before distributing this skill.";

export function ScriptLabel(): JSX.Element {
  return (
    <SimpleTooltip tooltip={SCRIPT_HINT}>
      <span className="text-muted-foreground shrink-0 font-mono text-[11px] tracking-wide uppercase">
        Script
      </span>
    </SimpleTooltip>
  );
}

/** The navigable list of a skill version's files, SKILL.md first. */
export function SkillFileList({
  files,
  manifestSizeBytes,
  selectedPath,
  onSelect,
}: {
  files: SkillFile[];
  manifestSizeBytes: number;
  selectedPath: string;
  onSelect: (path: string) => void;
}): JSX.Element {
  const rows = [
    { path: SKILL_MANIFEST_PATH, sizeBytes: manifestSizeBytes },
    ...files,
  ];
  return (
    <nav aria-label="Skill files">
      <ul className="space-y-px">
        {rows.map((row) => {
          const selected = row.path === selectedPath;
          return (
            <li key={row.path}>
              <button
                type="button"
                aria-current={selected ? "true" : undefined}
                onClick={() => onSelect(row.path)}
                className={cn(
                  "flex w-full items-center gap-2 px-2 py-1.5 text-left text-sm",
                  "hover:bg-muted/60 focus-visible:ring-ring focus-visible:ring-2 focus-visible:outline-none",
                  selected && "bg-muted font-medium",
                )}
              >
                <span
                  className="min-w-0 flex-1 truncate font-mono"
                  title={row.path}
                >
                  {row.path}
                </span>
                {isSkillScript(row.path) && <ScriptLabel />}
                <span className="text-muted-foreground shrink-0 font-mono text-xs">
                  {formatFileSize(row.sizeBytes)}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

/** Fetches a supporting file and shows it as Markdown, code, an image, or metadata. */
export function SkillFileViewer({
  skillId,
  versionId,
  file,
}: {
  skillId: string;
  versionId: string;
  file: SkillFile;
}): JSX.Element {
  const query = useSkillVersionFile(
    { skillId, versionId, path: file.path },
    undefined,
    // A version's files never change, so a fetched file stays fresh.
    { staleTime: Infinity, throwOnError: false },
  );
  const bytes = useMemo(
    () => (query.data ? bytesFromContent(query.data.content) : null),
    [query.data],
  );

  return (
    <div className="min-w-0 space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <p className="truncate font-mono text-sm" title={file.path}>
            {file.path}
          </p>
          <Text small muted>
            {describeContentType(file.contentType)} ·{" "}
            {formatFileSize(file.sizeBytes)}
            {isSkillScript(file.path) && " · agents can run this file"}
          </Text>
        </div>
        <Button
          size="sm"
          variant="secondary"
          disabled={!bytes}
          onClick={() => {
            if (bytes) downloadSkillFile(file.path, bytes, file.contentType);
          }}
        >
          <Download />
          Download
        </Button>
      </div>
      {query.isPending && <SkeletonParagraph lines={8} />}
      {query.error && !query.data && (
        <ErrorAlert title="Unable to load this file" error={query.error} />
      )}
      {bytes && <FilePreview file={file} bytes={bytes} />}
    </div>
  );
}

function FilePreview({
  file,
  bytes,
}: {
  file: SkillFile;
  bytes: Uint8Array;
}): JSX.Element {
  const text = isLikelyTextFile(file) ? decodeTextContent(bytes) : null;
  if (
    file.contentType.startsWith("image/") &&
    !file.path.toLowerCase().endsWith(".svg")
  ) {
    return <ImagePreview file={file} bytes={bytes} />;
  }
  if (text === null) {
    return (
      <InlineEmptyState
        icon="file"
        heading="No preview for this file type"
        description="Download the file to inspect it."
      />
    );
  }
  if (bytes.byteLength > MAX_PREVIEW_BYTES) {
    return (
      <InlineEmptyState
        icon="file-text"
        heading="Too large to preview"
        description={`Files over ${formatFileSize(MAX_PREVIEW_BYTES)} are not shown here. Download it to read it.`}
      />
    );
  }
  if (text.trim() === "") {
    return (
      <Text small muted>
        This file is empty.
      </Text>
    );
  }
  if (isMarkdownFile(file.path)) {
    return (
      <div className="overflow-x-auto">
        <Markdown className="text-sm">{text}</Markdown>
      </div>
    );
  }
  return (
    <CodeBlock language={languageForPath(file.path)} copyLabel={file.path}>
      {text}
    </CodeBlock>
  );
}

function ImagePreview({
  file,
  bytes,
}: {
  file: SkillFile;
  bytes: Uint8Array;
}): JSX.Element | null {
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    const next = URL.createObjectURL(
      new Blob([bytes.slice().buffer], { type: file.contentType }),
    );
    setUrl(next);
    return () => URL.revokeObjectURL(next);
  }, [bytes, file.contentType]);
  if (!url) return null;
  return (
    <img
      src={url}
      alt={file.path}
      className="max-h-96 max-w-full border object-contain"
    />
  );
}
