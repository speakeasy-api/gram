import { FileDiffOptions, ThemeTypes } from "@pierre/diffs";
import { useEffect, useState } from "react";

import { HighlightProvider } from "@/components/diffs/provider";
import { MultiFileDiff } from "@pierre/diffs/react";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { useFetcher } from "@/contexts/Fetcher";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";

type VersionDiffRef = {
  assetId?: string | null;
  label: string;
  versionId: string;
};

type Props = {
  baseline: VersionDiffRef | null;
  className?: string;
  projectId: string;
  target: VersionDiffRef | null;
};

type ZipFile = {
  binary: boolean;
  bytes: Uint8Array;
  size: number;
};

type DiffEntry = {
  binary: boolean;
  inlineDisabledReason?: string;
  newFile?: ZipFile;
  newText?: string;
  oldFile?: ZipFile;
  oldText?: string;
  path: string;
  status: "added" | "changed" | "removed";
};

type DiffResult = {
  baselineLabel: string;
  entries: DiffEntry[];
  summary: {
    added: number;
    changed: number;
    removed: number;
    unchanged: number;
  };
  targetLabel: string;
};

const MAX_COMPRESSED_ZIP_BYTES = 10 * 1024 * 1024;
const MAX_DECOMPRESSED_ZIP_BYTES = 25 * 1024 * 1024;
const MAX_FILES_PER_ARCHIVE = 1000;
const MAX_PER_FILE_INLINE_TEXT_BYTES = 512 * 1024;
const MAX_TOTAL_INLINE_TEXT_BYTES = 3 * 1024 * 1024;

const diffOptions: FileDiffOptions<undefined> = {
  theme: { dark: "pierre-dark", light: "pierre-light" },
  themeType: "system",
  diffStyle: "split",
  disableFileHeader: true,
  disableLineNumbers: true,
};

class GuardrailError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "GuardrailError";
  }
}

export function SkillVersionDiffPanel({
  target,
  baseline,
  projectId,
  className,
}: Props) {
  const { fetch: authFetch } = useFetcher();
  const { theme } = useMoonshineConfig();
  const [selectedPath, setSelectedPath] = useState<string | null>(null);

  const query = useQuery({
    queryKey: [
      "skill-version-diff",
      projectId,
      target?.versionId,
      target?.assetId,
      baseline?.versionId,
      baseline?.assetId,
    ],
    enabled: Boolean(target?.assetId),
    queryFn: async (): Promise<DiffResult> => {
      if (!target?.assetId) {
        throw new Error("Target version has no asset");
      }

      const targetZip = await fetchSkillZip(
        authFetch,
        target.assetId,
        projectId,
        target.label,
      );

      const baselineZip = baseline?.assetId
        ? await fetchSkillZip(
            authFetch,
            baseline.assetId,
            projectId,
            baseline.label,
          )
        : null;

      const targetFiles = await unzipFiles(targetZip, target.label);
      const baselineFiles = baselineZip
        ? await unzipFiles(baselineZip, baseline?.label ?? "No active version")
        : new Map<string, ZipFile>();

      return buildDiffResult({
        baselineFiles,
        baselineLabel: baseline?.label ?? "No active version",
        targetFiles,
        targetLabel: target.label,
      });
    },
  });

  const entries = query.data?.entries ?? [];

  useEffect(() => {
    if (entries.length === 0) {
      setSelectedPath(null);
      return;
    }

    if (
      !selectedPath ||
      !entries.some((entry) => entry.path === selectedPath)
    ) {
      setSelectedPath(entries[0].path);
    }
  }, [entries, selectedPath]);

  const selectedEntry =
    entries.find((entry) => entry.path === selectedPath) ?? entries[0] ?? null;

  const themeType: ThemeTypes =
    theme === "light" ? "light" : theme === "dark" ? "dark" : "system";

  return (
    <div
      className={cn("border-border bg-card rounded-xl border p-4", className)}
    >
      <div className="mb-3">
        <Type variant="subheading">Version diff</Type>
        <Type small muted>
          {query.data?.baselineLabel ?? baseline?.label ?? "No active version"}{" "}
          → {query.data?.targetLabel ?? target?.label ?? "Selected version"}
        </Type>
      </div>

      {!target ? (
        <PanelHint text="Select a version to compare." />
      ) : !target.assetId ? (
        <PanelHint text="Selected version has no downloadable asset." />
      ) : query.isPending ? (
        <PanelHint text="Loading diff…" />
      ) : query.error ? (
        <div className="border-destructive/30 bg-destructive/5 rounded-lg border p-3">
          <Type small className="text-destructive font-medium">
            Unable to build diff
          </Type>
          <Type small muted className="mt-1 block">
            {query.error.message}
          </Type>
        </div>
      ) : entries.length === 0 ? (
        <div className="border-border bg-muted/20 rounded-lg border p-4">
          <Type small>No content differences found.</Type>
          <Type small muted className="mt-1 block">
            Both versions contain the same files and bytes.
          </Type>
        </div>
      ) : (
        <>
          <div className="mb-3 flex flex-wrap gap-2">
            <SummaryPill label="Added" value={query.data.summary.added} />
            <SummaryPill label="Changed" value={query.data.summary.changed} />
            <SummaryPill label="Removed" value={query.data.summary.removed} />
          </div>

          <div className="grid gap-3 md:grid-cols-[minmax(220px,260px)_minmax(0,1fr)]">
            <div className="border-border bg-muted/20 max-h-[560px] overflow-auto rounded-lg border p-2">
              <div className="space-y-1">
                {entries.map((entry) => (
                  <button
                    key={entry.path}
                    type="button"
                    onClick={() => setSelectedPath(entry.path)}
                    className={cn(
                      "w-full rounded-md border px-2 py-1.5 text-left",
                      selectedEntry?.path === entry.path
                        ? "border-primary bg-primary/5"
                        : "hover:border-border hover:bg-background/70 border-transparent",
                    )}
                  >
                    <Type
                      small
                      className="truncate font-mono"
                      title={entry.path}
                    >
                      {entry.path}
                    </Type>
                    <Type
                      small
                      muted
                      className={cn(
                        "mt-0.5 block capitalize",
                        entry.status === "added" && "text-emerald-500",
                        entry.status === "removed" && "text-orange-500",
                        entry.status === "changed" && "text-blue-500",
                      )}
                    >
                      {entry.status}
                      {entry.binary ? " • binary" : ""}
                    </Type>
                  </button>
                ))}
              </div>
            </div>

            <div className="border-border bg-background min-h-[320px] rounded-lg border p-3">
              {!selectedEntry ? (
                <PanelHint text="Select a file to inspect." />
              ) : selectedEntry.binary ? (
                <BinaryMetadata entry={selectedEntry} />
              ) : selectedEntry.inlineDisabledReason ? (
                <PanelHint text={selectedEntry.inlineDisabledReason} />
              ) : (
                <HighlightProvider>
                  <MultiFileDiff
                    oldFile={{
                      name: query.data.baselineLabel,
                      contents: selectedEntry.oldText ?? "",
                      lang: guessLanguage(selectedEntry.path),
                    }}
                    newFile={{
                      name: query.data.targetLabel,
                      contents: selectedEntry.newText ?? "",
                      lang: guessLanguage(selectedEntry.path),
                    }}
                    options={{ ...diffOptions, themeType }}
                  />
                </HighlightProvider>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  );
}

function SummaryPill({ label, value }: { label: string; value: number }) {
  return (
    <span className="border-border bg-muted/30 inline-flex items-center rounded-full border px-2 py-0.5">
      <Type small muted>
        {label}: {value}
      </Type>
    </span>
  );
}

function PanelHint({ text }: { text: string }) {
  return (
    <div className="border-border bg-muted/20 rounded-lg border p-4">
      <Type small muted>
        {text}
      </Type>
    </div>
  );
}

function BinaryMetadata({ entry }: { entry: DiffEntry }) {
  return (
    <div className="space-y-2">
      <Type small className="font-medium">
        Binary file
      </Type>
      <Type small muted>
        Inline binary diff is disabled. Compare metadata instead.
      </Type>
      <div className="border-border bg-muted/20 rounded-lg border p-3">
        <Type small className="font-mono" title={entry.path}>
          {entry.path}
        </Type>
        <Type small muted className="mt-1 block">
          Old: {entry.oldFile ? formatBytes(entry.oldFile.size) : "missing"}
        </Type>
        <Type small muted className="block">
          New: {entry.newFile ? formatBytes(entry.newFile.size) : "missing"}
        </Type>
      </div>
    </div>
  );
}

async function fetchSkillZip(
  authFetch: ReturnType<typeof useFetcher>["fetch"],
  assetId: string,
  projectId: string,
  label: string,
): Promise<Uint8Array> {
  const params = new URLSearchParams({ id: assetId, project_id: projectId });
  const response = await authFetch(
    `/rpc/assets.serveSkill?${params.toString()}`,
    {
      method: "GET",
    },
  );

  if (!response.ok) {
    throw new Error(
      `Failed to download ${label}: ${response.status} ${response.statusText}`,
    );
  }

  const bytes = new Uint8Array(await response.arrayBuffer());
  if (bytes.byteLength > MAX_COMPRESSED_ZIP_BYTES) {
    throw new GuardrailError(
      `${label} archive exceeds ${formatBytes(MAX_COMPRESSED_ZIP_BYTES)} compressed limit`,
    );
  }

  return bytes;
}

async function unzipFiles(
  zipBytes: Uint8Array,
  label: string,
): Promise<Map<string, ZipFile>> {
  const declared = readZipDeclaredStats(zipBytes, label);
  if (declared.fileCount > MAX_FILES_PER_ARCHIVE) {
    throw new GuardrailError(
      `${label} archive exceeds ${MAX_FILES_PER_ARCHIVE} file limit`,
    );
  }
  if (declared.totalUncompressedBytes > MAX_DECOMPRESSED_ZIP_BYTES) {
    throw new GuardrailError(
      `${label} archive exceeds ${formatBytes(MAX_DECOMPRESSED_ZIP_BYTES)} decompressed limit`,
    );
  }

  const { unzipSync } = await import("fflate");
  let unzipped: Record<string, Uint8Array>;

  try {
    unzipped = unzipSync(zipBytes);
  } catch (error) {
    throw new Error(
      `Could not read ${label} archive: ${
        error instanceof Error ? error.message : "invalid zip"
      }`,
    );
  }

  const files = new Map<string, ZipFile>();
  let totalDecompressedBytes = 0;

  for (const [rawPath, bytes] of Object.entries(unzipped)) {
    const path = normalizePath(rawPath);
    if (!path || path.endsWith("/")) {
      continue;
    }

    totalDecompressedBytes += bytes.byteLength;
    if (totalDecompressedBytes > MAX_DECOMPRESSED_ZIP_BYTES) {
      throw new GuardrailError(
        `${label} archive exceeds ${formatBytes(MAX_DECOMPRESSED_ZIP_BYTES)} decompressed limit`,
      );
    }

    files.set(path, {
      bytes,
      size: bytes.byteLength,
      binary: isProbablyBinary(bytes),
    });
  }

  if (files.size > MAX_FILES_PER_ARCHIVE) {
    throw new GuardrailError(
      `${label} archive exceeds ${MAX_FILES_PER_ARCHIVE} file limit`,
    );
  }

  return files;
}

function buildDiffResult({
  baselineFiles,
  targetFiles,
  baselineLabel,
  targetLabel,
}: {
  baselineFiles: Map<string, ZipFile>;
  baselineLabel: string;
  targetFiles: Map<string, ZipFile>;
  targetLabel: string;
}): DiffResult {
  const decoder = new TextDecoder("utf-8");
  const paths = Array.from(
    new Set([...baselineFiles.keys(), ...targetFiles.keys()]),
  )
    .map(normalizePath)
    .filter(Boolean)
    .sort((a, b) => a.localeCompare(b));

  let added = 0;
  let changed = 0;
  let removed = 0;
  let unchanged = 0;
  let remainingInlineBudget = MAX_TOTAL_INLINE_TEXT_BYTES;

  const entries: DiffEntry[] = [];

  for (const path of paths) {
    const oldFile = baselineFiles.get(path);
    const newFile = targetFiles.get(path);

    if (!oldFile && !newFile) {
      continue;
    }

    const status: DiffEntry["status"] = !oldFile
      ? "added"
      : !newFile
        ? "removed"
        : "changed";

    const bytesEqual =
      oldFile && newFile ? areBytesEqual(oldFile.bytes, newFile.bytes) : false;

    if (oldFile && newFile && bytesEqual) {
      unchanged += 1;
      continue;
    }

    if (status === "added") {
      added += 1;
    } else if (status === "removed") {
      removed += 1;
    } else {
      changed += 1;
    }

    const binary = Boolean(oldFile?.binary || newFile?.binary);
    const entry: DiffEntry = { path, status, oldFile, newFile, binary };

    if (!binary) {
      const inlineBytes = (oldFile?.size ?? 0) + (newFile?.size ?? 0);
      if (inlineBytes > MAX_PER_FILE_INLINE_TEXT_BYTES) {
        entry.inlineDisabledReason =
          "Inline diff disabled: file exceeds per-file text budget.";
      } else if (inlineBytes > remainingInlineBudget) {
        entry.inlineDisabledReason =
          "Inline diff disabled: total text diff budget exceeded for this comparison.";
      } else {
        remainingInlineBudget -= inlineBytes;
        entry.oldText = oldFile ? decoder.decode(oldFile.bytes) : "";
        entry.newText = newFile ? decoder.decode(newFile.bytes) : "";
      }
    }

    entries.push(entry);
  }

  return {
    baselineLabel,
    targetLabel,
    entries,
    summary: { added, changed, removed, unchanged },
  };
}

function readZipDeclaredStats(
  zipBytes: Uint8Array,
  label: string,
): { fileCount: number; totalUncompressedBytes: number } {
  const eocdSignature = 0x06054b50;
  const centralSignature = 0x02014b50;
  const minEocdSize = 22;

  if (zipBytes.byteLength < minEocdSize) {
    throw new Error(`Could not read ${label} archive: invalid zip`);
  }

  const view = new DataView(
    zipBytes.buffer,
    zipBytes.byteOffset,
    zipBytes.byteLength,
  );

  let eocdOffset = -1;
  const searchStart = Math.max(0, zipBytes.byteLength - (0xffff + minEocdSize));
  for (let i = zipBytes.byteLength - minEocdSize; i >= searchStart; i -= 1) {
    if (view.getUint32(i, true) === eocdSignature) {
      eocdOffset = i;
      break;
    }
  }

  if (eocdOffset < 0) {
    throw new Error(`Could not read ${label} archive: missing EOCD`);
  }

  const totalEntries = view.getUint16(eocdOffset + 10, true);
  const centralDirectorySize = view.getUint32(eocdOffset + 12, true);
  const centralDirectoryOffset = view.getUint32(eocdOffset + 16, true);

  if (
    totalEntries === 0xffff ||
    centralDirectorySize === 0xffffffff ||
    centralDirectoryOffset === 0xffffffff
  ) {
    throw new GuardrailError(`${label} archive uses unsupported ZIP64 format`);
  }

  if (
    centralDirectoryOffset + centralDirectorySize > zipBytes.byteLength ||
    centralDirectoryOffset < 0
  ) {
    throw new Error(
      `Could not read ${label} archive: invalid central directory`,
    );
  }

  let fileCount = 0;
  let totalUncompressedBytes = 0;
  let ptr = centralDirectoryOffset;
  const centralEnd = centralDirectoryOffset + centralDirectorySize;

  while (ptr < centralEnd) {
    if (ptr + 46 > centralEnd) {
      throw new Error(
        `Could not read ${label} archive: truncated central entry`,
      );
    }
    if (view.getUint32(ptr, true) !== centralSignature) {
      throw new Error(`Could not read ${label} archive: invalid central entry`);
    }

    const uncompressedSize = view.getUint32(ptr + 24, true);
    const nameLength = view.getUint16(ptr + 28, true);
    const extraLength = view.getUint16(ptr + 30, true);
    const commentLength = view.getUint16(ptr + 32, true);

    fileCount += 1;
    totalUncompressedBytes += uncompressedSize;

    ptr += 46 + nameLength + extraLength + commentLength;
  }

  if (ptr !== centralEnd) {
    throw new Error(
      `Could not read ${label} archive: malformed central directory`,
    );
  }

  if (fileCount !== totalEntries) {
    throw new Error(`Could not read ${label} archive: entry count mismatch`);
  }

  return { fileCount, totalUncompressedBytes };
}

function normalizePath(path: string): string {
  return path.replace(/\\/g, "/").replace(/^\.\//, "");
}

function areBytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.byteLength !== b.byteLength) {
    return false;
  }

  for (let i = 0; i < a.byteLength; i += 1) {
    if (a[i] !== b[i]) {
      return false;
    }
  }

  return true;
}

function isProbablyBinary(bytes: Uint8Array): boolean {
  const sampleSize = Math.min(bytes.byteLength, 8000);
  if (sampleSize === 0) {
    return false;
  }

  let suspicious = 0;
  for (let i = 0; i < sampleSize; i += 1) {
    const c = bytes[i];
    if (c === 0) {
      return true;
    }
    const isControl = c < 7 || (c > 14 && c < 32) || c === 127;
    if (isControl) {
      suspicious += 1;
    }
  }

  return suspicious / sampleSize > 0.3;
}

function guessLanguage(path: string): string | undefined {
  const file = path.toLowerCase();

  if (file.endsWith(".json")) return "json";
  if (file.endsWith(".yaml") || file.endsWith(".yml")) return "yaml";
  if (file.endsWith(".md")) return "markdown";
  if (file.endsWith(".ts") || file.endsWith(".tsx")) return "typescript";
  if (file.endsWith(".js") || file.endsWith(".mjs") || file.endsWith(".cjs"))
    return "javascript";
  if (file.endsWith(".go")) return "go";
  if (file.endsWith(".sh")) return "bash";
  if (file.endsWith(".py")) return "python";

  return undefined;
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B";
  }

  const units = ["B", "KB", "MB", "GB"];
  const exponent = Math.min(
    Math.floor(Math.log(value) / Math.log(1024)),
    units.length - 1,
  );
  const size = value / 1024 ** exponent;
  return `${size.toFixed(size >= 10 || exponent === 0 ? 0 : 1)} ${units[exponent]}`;
}
