import type { DraftDocument } from "@/pages/context/mock-data";

export type ApiDraft = {
  id: string;
  project_id: string;
  organization_id: string;
  file_path: string;
  content: string | null;
  operation: "create" | "update" | "delete";
  status: "open" | "published" | "rejected";
  source: string | null;
  author_type: string | null;
  labels: string[] | null;
  commit_sha: string | null;
  created_at: string;
  updated_at: string;
};

export function apiDraftToDisplay(draft: ApiDraft): DraftDocument {
  throw new Error("not implemented");
}

export async function fetchDrafts(
  _projectId: string,
  _token: string,
  _status?: string,
): Promise<DraftDocument[]> {
  throw new Error("not implemented");
}

export async function publishDrafts(
  _projectId: string,
  _token: string,
  _draftIds: string[],
): Promise<{ commitSha: string }> {
  throw new Error("not implemented");
}
