export const MAX_REPOSITORY_MIGRATION_ENTRIES = 50;
export const MAX_REPOSITORY_EVIDENCE_LENGTH = 1024;
export const MAX_REPOSITORY_REASON_LENGTH = 1024;
const MAX_IMPORT_LENGTH = 128 * 1024;
const PROJECT_ID =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const MAX_REPOSITORY_ID = 9223372036854775807n;

export type RepositoryEvidence = {
  projectId: string;
  repositoryId: string;
  evidence: string;
};

export type RepositoryVerification = {
  projectId: string;
  status: string;
  message?: string | undefined;
};

export function repositoryEvidence(
  projectId: unknown,
  repositoryId: unknown,
  evidence: unknown,
): RepositoryEvidence {
  if (
    typeof projectId !== "string" ||
    !PROJECT_ID.test(projectId) ||
    projectId === "00000000-0000-0000-0000-000000000000"
  ) {
    throw new Error("Enter the exact project UUID.");
  }
  if (
    typeof repositoryId !== "string" ||
    !/^[1-9][0-9]{0,18}$/.test(repositoryId) ||
    BigInt(repositoryId) > MAX_REPOSITORY_ID
  ) {
    throw new Error(
      "Enter the GitHub repository ID as a positive decimal string.",
    );
  }
  if (
    typeof evidence !== "string" ||
    !evidence.trim() ||
    new TextEncoder().encode(evidence.trim()).length >
      MAX_REPOSITORY_EVIDENCE_LENGTH
  ) {
    throw new Error(
      `Enter a trusted historical reference of at most ${MAX_REPOSITORY_EVIDENCE_LENGTH} bytes.`,
    );
  }
  return {
    projectId: projectId.toLowerCase(),
    repositoryId,
    evidence: evidence.trim(),
  };
}

export function parseRepositoryEvidence(source: string): RepositoryEvidence[] {
  if (source.length > MAX_IMPORT_LENGTH)
    throw new Error("The import is too large.");
  let rows: unknown;
  try {
    rows = JSON.parse(source);
  } catch {
    throw new Error("Paste a valid JSON array of evidence records.");
  }
  if (
    !Array.isArray(rows) ||
    rows.length === 0 ||
    rows.length > MAX_REPOSITORY_MIGRATION_ENTRIES
  ) {
    throw new Error(
      `Import 1–${MAX_REPOSITORY_MIGRATION_ENTRIES} records at a time.`,
    );
  }
  const projectIds = new Set<string>();
  const repositoryIds = new Set<string>();
  return rows.map((row: unknown, index) => {
    try {
      if (!row || typeof row !== "object" || Array.isArray(row)) {
        throw new Error(
          "Enter an evidence record with project_id, repository_id, and evidence.",
        );
      }
      for (const key of Object.keys(row)) {
        if (
          key !== "project_id" &&
          key !== "repository_id" &&
          key !== "evidence"
        ) {
          throw new Error(`Unexpected field: ${key}.`);
        }
      }
      const fields = row as Record<string, unknown>;
      const entry = repositoryEvidence(
        fields.project_id,
        fields.repository_id,
        fields.evidence,
      );
      if (projectIds.has(entry.projectId))
        throw new Error("Duplicate project UUID.");
      if (repositoryIds.has(entry.repositoryId))
        throw new Error("Duplicate GitHub repository ID.");
      projectIds.add(entry.projectId);
      repositoryIds.add(entry.repositoryId);
      return entry;
    } catch (error) {
      throw new Error(
        `Record ${index + 1}: ${error instanceof Error ? error.message : String(error)}`,
      );
    }
  });
}

// Only use decisions from the preview of these exact entries. Missing or
// ambiguous decisions are not permission to write.
export function verifiedRepositoryEvidence(
  entries: readonly RepositoryEvidence[],
  results: readonly RepositoryVerification[],
): RepositoryEvidence[] {
  const verified = new Map<string, boolean>();
  for (const result of results) {
    verified.set(
      result.projectId,
      !verified.has(result.projectId) && result.status === "verified",
    );
  }
  return entries.filter((entry) => verified.get(entry.projectId));
}
