import { Alert, AlertDescription, ErrorAlert } from "@/components/ui/Alert";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { useSkillVersionFile } from "@gram/client/react-query/skillVersionFile.js";
import { lazy, Suspense, useState, type ReactNode } from "react";
import { ErrorBoundary } from "react-error-boundary";
import { MAX_PREVIEW_BYTES, ScriptLabel } from "./SkillFiles";
import {
  bytesFromContent,
  decodeTextContent,
  diffSkillFiles,
  formatFileSize,
  isLikelyTextFile,
  isSkillScript,
  languageForPath,
  SKILL_MANIFEST_PATH,
  type SkillFileChange,
  type SkillFileChangeStatus,
} from "./skill-files";

const SkillTextDiff = lazy(() => import("./SkillTextDiff"));

const STATUS_LABEL: Record<SkillFileChangeStatus, string> = {
  added: "Added",
  removed: "Removed",
  changed: "Changed",
  unchanged: "Unchanged",
};

const STATUS_CLASS: Record<SkillFileChangeStatus, string> = {
  added: "text-success",
  removed: "text-destructive",
  changed: "text-warning",
  unchanged: "text-muted-foreground",
};

/**
 * Compares two skill versions: which files were added, removed, or changed,
 * and a line diff of the selected file.
 */
export function SkillVersionDiff({
  skillId,
  older,
  newer,
}: {
  skillId: string;
  older: SkillVersion;
  newer: SkillVersion;
}): JSX.Element {
  const [selectedPath, setSelectedPath] = useState(SKILL_MANIFEST_PATH);
  const oldLabel = older.canonicalSha256.slice(0, 8);
  const newLabel = newer.canonicalSha256.slice(0, 8);
  const fileChanges = diffSkillFiles(older.files, newer.files);
  const hasFiles = fileChanges.length > 0;
  const manifestChanged = older.content !== newer.content;
  const changedFiles = fileChanges.filter((c) => c.status !== "unchanged");
  const unchangedCount = fileChanges.length - changedFiles.length;
  const selectedChange = changedFiles.find((c) => c.path === selectedPath);

  return (
    <div className="space-y-3">
      <Text small muted mono className="text-xs tracking-wider">
        Diff · {oldLabel} → {newLabel}
      </Text>
      {hasFiles && (
        <div className="space-y-2">
          <ul
            aria-label="Changed files"
            className="divide-border divide-y border"
          >
            <ChangeRow
              path={SKILL_MANIFEST_PATH}
              status={manifestChanged ? "changed" : "unchanged"}
              selected={selectedPath === SKILL_MANIFEST_PATH}
              onSelect={() => setSelectedPath(SKILL_MANIFEST_PATH)}
            />
            {changedFiles.map((change) => (
              <ChangeRow
                key={change.path}
                path={change.path}
                status={change.status}
                selected={selectedPath === change.path}
                onSelect={() => setSelectedPath(change.path)}
                detail={sizeDetail(change)}
              />
            ))}
          </ul>
          {unchangedCount > 0 && (
            <Text small muted>
              {unchangedCount} unchanged supporting{" "}
              {unchangedCount === 1 ? "file" : "files"} not shown.
            </Text>
          )}
        </div>
      )}
      <DiffBoundary>
        {selectedChange ? (
          <SkillFileDiff
            key={selectedChange.path}
            skillId={skillId}
            older={older}
            newer={newer}
            change={selectedChange}
          />
        ) : (
          <SkillTextDiff
            oldContent={older.content}
            newContent={newer.content}
            oldLabel={
              hasFiles ? `${SKILL_MANIFEST_PATH} @ ${oldLabel}` : oldLabel
            }
            newLabel={
              hasFiles ? `${SKILL_MANIFEST_PATH} @ ${newLabel}` : newLabel
            }
          />
        )}
      </DiffBoundary>
    </div>
  );
}

function sizeDetail(change: SkillFileChange): string {
  if (change.status === "added" && change.after)
    return formatFileSize(change.after.sizeBytes);
  if (change.status === "removed" && change.before)
    return formatFileSize(change.before.sizeBytes);
  if (change.before && change.after)
    return `${formatFileSize(change.before.sizeBytes)} → ${formatFileSize(change.after.sizeBytes)}`;
  return "";
}

function ChangeRow({
  path,
  status,
  selected,
  onSelect,
  detail,
}: {
  path: string;
  status: SkillFileChangeStatus;
  selected: boolean;
  onSelect: () => void;
  detail?: string;
}): JSX.Element {
  return (
    <li>
      <button
        type="button"
        aria-current={selected ? "true" : undefined}
        onClick={onSelect}
        className={cn(
          "flex w-full items-center gap-3 px-3 py-1.5 text-left text-sm",
          "hover:bg-muted/60 focus-visible:ring-ring focus-visible:ring-2 focus-visible:outline-none",
          selected && "bg-muted font-medium",
        )}
      >
        <span
          className={cn(
            "w-20 shrink-0 font-mono text-xs uppercase",
            STATUS_CLASS[status],
          )}
        >
          {STATUS_LABEL[status]}
        </span>
        <span className="min-w-0 flex-1 truncate font-mono" title={path}>
          {path}
        </span>
        {isSkillScript(path) && <ScriptLabel />}
        {detail && (
          <span className="text-muted-foreground shrink-0 font-mono text-xs">
            {detail}
          </span>
        )}
      </button>
    </li>
  );
}

function DiffBoundary({ children }: { children: ReactNode }): JSX.Element {
  return (
    <ErrorBoundary
      fallback={
        <Alert variant="warning">
          <AlertDescription>
            Unable to load the diff. Try again.
          </AlertDescription>
        </Alert>
      }
    >
      <Suspense fallback={<div className="h-80 w-full" />}>{children}</Suspense>
    </ErrorBoundary>
  );
}

function useVersionFileText(
  skillId: string,
  version: SkillVersion,
  file: SkillFileChange["before"],
  enabled: boolean,
) {
  const fetchFile = enabled && !!file;
  const query = useSkillVersionFile(
    { skillId, versionId: version.id, path: file?.path ?? "" },
    undefined,
    { enabled: fetchFile, staleTime: Infinity, throwOnError: false },
  );
  const text = query.data
    ? decodeTextContent(bytesFromContent(query.data.content))
    : null;
  return {
    loading: fetchFile && query.isPending,
    error: query.error,
    // A missing side diffs against an empty file.
    text: file ? text : "",
  };
}

function NoLineDiff({ change }: { change: SkillFileChange }): JSX.Element {
  return (
    <Text small muted>
      {change.path} is not a text file, so there is no line diff. Size:{" "}
      {sizeDetail(change)}.
    </Text>
  );
}

/** A line diff of one supporting file between two versions. */
function SkillFileDiff({
  skillId,
  older,
  newer,
  change,
}: {
  skillId: string;
  older: SkillVersion;
  newer: SkillVersion;
  change: SkillFileChange;
}): JSX.Element {
  // Decide from the metadata whether a line diff is possible, so binaries and
  // large files are never downloaded just to be discarded.
  const sides = [change.before, change.after].filter((f) => f !== undefined);
  const notText = sides.some((f) => !isLikelyTextFile(f));
  const tooLarge = sides.some((f) => f.sizeBytes > MAX_PREVIEW_BYTES);
  const canDiff = !notText && !tooLarge;
  const before = useVersionFileText(skillId, older, change.before, canDiff);
  const after = useVersionFileText(skillId, newer, change.after, canDiff);
  const error = before.error ?? after.error;

  if (notText) {
    return <NoLineDiff change={change} />;
  }
  if (tooLarge) {
    return (
      <Text small muted>
        {change.path} is too large to diff here. Files over{" "}
        {formatFileSize(MAX_PREVIEW_BYTES)} have no line diff. Size:{" "}
        {sizeDetail(change)}.
      </Text>
    );
  }
  if (error) {
    return <ErrorAlert title="Unable to load this file" error={error} />;
  }
  if (before.loading || after.loading) return <SkeletonParagraph lines={8} />;
  if (before.text === null || after.text === null) {
    return <NoLineDiff change={change} />;
  }
  return (
    <SkillTextDiff
      oldContent={before.text}
      newContent={after.text}
      oldLabel={`${change.path} @ ${older.canonicalSha256.slice(0, 8)}`}
      newLabel={`${change.path} @ ${newer.canonicalSha256.slice(0, 8)}`}
      lang={languageForPath(change.path)}
    />
  );
}
