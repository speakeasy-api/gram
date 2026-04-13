import { describe, expect, it, vi, beforeEach } from "vitest";
import {
  apiDraftToDisplay,
  fetchDrafts,
  publishDrafts,
} from "../../hooks/useDrafts";
import type { ApiDraft } from "../../hooks/useDrafts";

const mockFetch = vi.fn();
globalThis.fetch = mockFetch;

beforeEach(() => {
  vi.clearAllMocks();
});

function makeApiDraft(overrides: Partial<ApiDraft> = {}): ApiDraft {
  return {
    id: "draft-1",
    project_id: "proj-1",
    organization_id: "org-1",
    file_path: "docs/guide.md",
    content: "# Guide\n\nUpdated content.",
    operation: "update",
    status: "open",
    source: "api",
    author_type: "human",
    labels: ["docs"],
    commit_sha: null,
    created_at: "2026-04-01T10:00:00Z",
    updated_at: "2026-04-01T12:00:00Z",
    ...overrides,
  };
}

describe("apiDraftToDisplay", () => {
  it("renders draft list from API", () => {
    const apiDraft = makeApiDraft();
    const display = apiDraftToDisplay(apiDraft);

    expect(display.id).toBe("draft-1");
    expect(display.filePath).toBe("docs/guide.md");
    expect(display.content).toBe("# Guide\n\nUpdated content.");
    expect(display.status).toBe("open");
    expect(display.authorType).toBe("human");
    expect(display.labels).toEqual(["docs"]);
  });

  it("shows diff between original and proposed content", () => {
    const apiDraft = makeApiDraft({
      operation: "update",
      file_path: "docs/guide.md",
      content: "# Updated Guide",
    });
    const display = apiDraftToDisplay(apiDraft);

    expect(display.filePath).toBe("docs/guide.md");
    expect(display.content).toBe("# Updated Guide");
  });

  it("distinguishes agent vs human authors", () => {
    const human = apiDraftToDisplay(makeApiDraft({ author_type: "human" }));
    expect(human.authorType).toBe("human");

    const agent = apiDraftToDisplay(makeApiDraft({ author_type: "agent" }));
    expect(agent.authorType).toBe("agent");
  });

  it("handles create operation as new doc", () => {
    const draft = apiDraftToDisplay(
      makeApiDraft({ operation: "create", file_path: "new/doc.md" }),
    );
    expect(draft.filePath).toBeNull();
    expect(draft.proposedPath).toBe("new/doc.md");
  });

  it("handles delete operation", () => {
    const draft = apiDraftToDisplay(
      makeApiDraft({ operation: "delete", content: null }),
    );
    expect(draft.content).toBe("");
  });
});

describe("fetchDrafts", () => {
  it("fetches and transforms drafts from API", async () => {
    const apiDrafts = [
      makeApiDraft(),
      makeApiDraft({ id: "draft-2", file_path: "other.md" }),
    ];
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ drafts: apiDrafts }),
    });

    const result = await fetchDrafts("proj-1", "bearer-token");

    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining("/rpc/corpus.listDrafts"),
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({}),
      }),
    );
    expect(result).toHaveLength(2);
    expect(result[0].id).toBe("draft-1");
    expect(result[1].id).toBe("draft-2");
  });
});

describe("publishDrafts", () => {
  it("publishes single draft", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ commit_sha: "abc123" }),
    });

    const result = await publishDrafts("proj-1", "bearer-token", ["draft-1"]);

    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining("/rpc/corpus.publishDrafts"),
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ draft_ids: ["draft-1"] }),
      }),
    );
    expect(result.commitSha).toBe("abc123");
  });

  it("batch publishes selected drafts", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ commit_sha: "def456" }),
    });

    const result = await publishDrafts("proj-1", "bearer-token", [
      "draft-1",
      "draft-2",
    ]);

    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining("/rpc/corpus.publishDrafts"),
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ draft_ids: ["draft-1", "draft-2"] }),
      }),
    );
    expect(result.commitSha).toBe("def456");
  });
});
