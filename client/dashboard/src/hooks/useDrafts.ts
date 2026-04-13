import { rpc } from "@/lib/rpc";
import type { DraftDocument } from "@/pages/context/mock-data";

export type ApiDraft = {
  id: string;
  project_id: string;
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
  const isCreate = draft.operation === "create";

  return {
    id: draft.id,
    title: draft.file_path.split("/").pop() ?? draft.file_path,
    author: draft.source ?? "unknown",
    authorType: (draft.author_type as "human" | "agent") ?? "human",
    createdAt: draft.created_at,
    updatedAt: draft.updated_at,
    filePath: isCreate ? null : draft.file_path,
    proposedPath: isCreate ? draft.file_path : undefined,
    content: draft.content ?? "",
    upvotes: 0,
    downvotes: 0,
    userVote: null,
    comments: [],
    status: draft.status,
    labels: draft.labels ?? [],
  };
}

export async function fetchDrafts(
  _projectId: string,
  _token?: string,
  status?: string,
): Promise<DraftDocument[]> {
  const result = await rpc<{ drafts: ApiDraft[] }>("corpus.listDrafts", {
    ...(status ? { status } : {}),
  });

  return result.drafts.map(apiDraftToDisplay);
}

export async function publishDrafts(
  _projectId: string,
  _token?: string,
  draftIds?: string[],
): Promise<{ commitSha: string }> {
  const result = await rpc<{ commit_sha: string }>("corpus.publishDrafts", {
    draft_ids: draftIds,
  });

  return { commitSha: result.commit_sha };
}
