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

const API_BASE = "/v1/projects";

export async function fetchDrafts(
  projectId: string,
  token: string,
  status?: string,
): Promise<DraftDocument[]> {
  let url = `${API_BASE}/${projectId}/corpus/drafts`;
  if (status) {
    url += `?status=${encodeURIComponent(status)}`;
  }

  const resp = await fetch(url, {
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
  });

  if (!resp.ok) {
    throw new Error(`Failed to fetch drafts: ${resp.status}`);
  }

  const apiDrafts: ApiDraft[] = await resp.json();
  return apiDrafts.map(apiDraftToDisplay);
}

export async function publishDrafts(
  projectId: string,
  token: string,
  draftIds: string[],
): Promise<{ commitSha: string }> {
  const resp = await fetch(`${API_BASE}/${projectId}/corpus/drafts/publish`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ draft_ids: draftIds }),
  });

  if (!resp.ok) {
    throw new Error(`Failed to publish drafts: ${resp.status}`);
  }

  const data = await resp.json();
  return { commitSha: data.commit_sha };
}
