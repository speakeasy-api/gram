import { createDashboardGramClient } from "@/lib/gram";
import type { CorpusDraftResult } from "@gram/client/models/components";
import type { DraftDocument } from "@/pages/context/mock-data";

export type ApiDraft = {
  id: string;
  project_id: string;
  file_path: string;
  title?: string | null;
  content: string | null;
  original_content?: string | null;
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
    title: draft.title ?? draft.file_path.split("/").pop() ?? draft.file_path,
    author: draft.source ?? "unknown",
    authorType: (draft.author_type as "human" | "agent") ?? "human",
    createdAt: draft.created_at,
    updatedAt: draft.updated_at,
    filePath: isCreate ? null : draft.file_path,
    proposedPath: isCreate ? draft.file_path : undefined,
    originalContent: draft.original_content ?? undefined,
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
  const client = createDashboardGramClient();
  const result = await client.corpus.listDrafts({
    listDraftsRequestBody: {
      ...(status ? { status } : {}),
    },
  });

  return result.drafts.map((draft: CorpusDraftResult) =>
    apiDraftToDisplay({
      author_type: draft.authorType ?? null,
      commit_sha: draft.commitSha ?? null,
      content: draft.content ?? null,
      created_at: draft.createdAt.toISOString(),
      file_path: draft.filePath,
      id: draft.id,
      labels: draft.labels ?? null,
      operation: draft.operation,
      original_content: draft.originalContent ?? null,
      project_id: draft.projectId,
      source: draft.source ?? null,
      status: draft.status,
      title: draft.title ?? null,
      updated_at: draft.updatedAt.toISOString(),
    }),
  );
}

export async function publishDrafts(
  _projectId: string,
  _token?: string,
  draftIds?: string[],
): Promise<{ commitSha: string }> {
  const client = createDashboardGramClient();
  const result = await client.corpus.publishDrafts({
    publishDraftsRequestBody: {
      draftIds: draftIds ?? [],
    },
  });

  return { commitSha: result.commitSha };
}

export async function saveDraft(
  _projectId: string,
  params: {
    draftId?: string;
    filePath: string;
    content: string;
    originalContent?: string;
    title?: string;
  },
): Promise<DraftDocument> {
  const client = createDashboardGramClient();

  if (params.draftId) {
    const result = await client.corpus.updateDraft({
      updateDraftRequestBody: {
        id: params.draftId,
        content: params.content,
      },
    });

    return apiDraftToDisplay({
      author_type: result.authorType ?? null,
      commit_sha: result.commitSha ?? null,
      content: result.content ?? null,
      created_at: result.createdAt.toISOString(),
      file_path: result.filePath,
      id: result.id,
      labels: result.labels ?? null,
      operation: result.operation,
      original_content: result.originalContent ?? null,
      project_id: result.projectId,
      source: result.source ?? null,
      status: result.status,
      title: result.title ?? null,
      updated_at: result.updatedAt.toISOString(),
    });
  }

  const result = await client.corpus.createDraft({
    createDraftRequestBody: {
      filePath: params.filePath,
      title: params.title,
      content: params.content,
      originalContent: params.originalContent,
      operation: "update",
      source: "dashboard",
      authorType: "human",
    },
  });

  return apiDraftToDisplay({
    author_type: result.authorType ?? null,
    commit_sha: result.commitSha ?? null,
    content: result.content ?? null,
    created_at: result.createdAt.toISOString(),
    file_path: result.filePath,
    id: result.id,
    labels: result.labels ?? null,
    operation: result.operation,
    original_content: result.originalContent ?? null,
    project_id: result.projectId,
    source: result.source ?? null,
    status: result.status,
    title: result.title ?? null,
    updated_at: result.updatedAt.toISOString(),
  });
}
