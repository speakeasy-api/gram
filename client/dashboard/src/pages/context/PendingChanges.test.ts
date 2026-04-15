import { describe, expect, it, vi, beforeEach } from "vitest";
import {
  apiDraftToDisplay,
  fetchDrafts,
  publishDrafts,
  saveDraft,
} from "../../hooks/useDrafts";
import type { ApiDraft } from "../../hooks/useDrafts";
import { extractFetchUrl } from "@/test-utils";

const mockFetch = vi.fn();
globalThis.fetch = mockFetch;

beforeEach(() => {
  vi.clearAllMocks();
});

function makeApiDraft(overrides: Partial<ApiDraft> = {}): ApiDraft {
  return {
    id: "draft-1",
    project_id: "proj-1",
    file_path: "docs/guide.md",
    title: "Guide",
    content: "# Guide\n\nUpdated content.",
    original_content: "# Guide\n\nOriginal content.",
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

function toDraftResponse(draft: ApiDraft) {
  return {
    author_type: draft.author_type ?? undefined,
    commit_sha: draft.commit_sha ?? undefined,
    content: draft.content ?? undefined,
    created_at: draft.created_at,
    file_path: draft.file_path,
    id: draft.id,
    labels: draft.labels ?? undefined,
    operation: draft.operation,
    original_content: draft.original_content ?? undefined,
    project_id: draft.project_id,
    source: draft.source ?? undefined,
    status: draft.status,
    title: draft.title ?? undefined,
    updated_at: draft.updated_at,
  };
}

describe("apiDraftToDisplay", () => {
  it("renders draft list from API", () => {
    const apiDraft = makeApiDraft();
    const display = apiDraftToDisplay(apiDraft);

    expect(display.id).toBe("draft-1");
    expect(display.filePath).toBe("docs/guide.md");
    expect(display.content).toBe("# Guide\n\nUpdated content.");
    expect(display.originalContent).toBe("# Guide\n\nOriginal content.");
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
    const requestBodies = new Map<string, Record<string, unknown>>();
    const apiDrafts = [
      makeApiDraft(),
      makeApiDraft({ id: "draft-2", file_path: "other.md" }),
    ];
    mockFetch.mockImplementationOnce(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);
      if (input instanceof Request) {
        requestBodies.set(
          url,
          (await input.clone().json()) as Record<string, unknown>,
        );
      }

      return new Response(
        JSON.stringify({
          drafts: apiDrafts.map(toDraftResponse),
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      );
    });

    const result = await fetchDrafts("proj-1", "bearer-token");

    const listCall = mockFetch.mock.calls.find((call: [RequestInfo | URL]) =>
      extractFetchUrl(call[0]).includes("/rpc/corpus.listDrafts"),
    );
    expect(listCall).toBeDefined();
    expect(requestBodies.get(extractFetchUrl(listCall?.[0]))).toEqual({});
    expect(result).toHaveLength(2);
    expect(result[0].id).toBe("draft-1");
    expect(result[1].id).toBe("draft-2");
    expect(result[0].originalContent).toBe("# Guide\n\nOriginal content.");
  });
});

describe("publishDrafts", () => {
  it("publishes single draft", async () => {
    const requestBodies = new Map<string, Record<string, unknown>>();
    mockFetch.mockImplementationOnce(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);
      if (input instanceof Request) {
        requestBodies.set(
          url,
          (await input.clone().json()) as Record<string, unknown>,
        );
      }

      return new Response(JSON.stringify({ commit_sha: "abc123" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    const result = await publishDrafts("proj-1", "bearer-token", ["draft-1"]);

    const publishCall = mockFetch.mock.calls.find((call: [RequestInfo | URL]) =>
      extractFetchUrl(call[0]).includes("/rpc/corpus.publishDrafts"),
    );
    expect(publishCall).toBeDefined();
    expect(requestBodies.get(extractFetchUrl(publishCall?.[0]))).toEqual({
      draft_ids: ["draft-1"],
    });
    expect(result.commitSha).toBe("abc123");
  });

  it("batch publishes selected drafts", async () => {
    const requestBodies = new Map<string, Record<string, unknown>>();
    mockFetch.mockImplementationOnce(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);
      if (input instanceof Request) {
        requestBodies.set(
          url,
          (await input.clone().json()) as Record<string, unknown>,
        );
      }

      return new Response(JSON.stringify({ commit_sha: "def456" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    const result = await publishDrafts("proj-1", "bearer-token", [
      "draft-1",
      "draft-2",
    ]);

    const publishCall = mockFetch.mock.calls.find((call: [RequestInfo | URL]) =>
      extractFetchUrl(call[0]).includes("/rpc/corpus.publishDrafts"),
    );
    expect(publishCall).toBeDefined();
    expect(requestBodies.get(extractFetchUrl(publishCall?.[0]))).toEqual({
      draft_ids: ["draft-1", "draft-2"],
    });
    expect(result.commitSha).toBe("def456");
  });
});

describe("saveDraft", () => {
  it("creates a new draft when no draft id is provided", async () => {
    const requestBodies = new Map<string, Record<string, unknown>>();
    mockFetch.mockImplementationOnce(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);
      if (input instanceof Request) {
        requestBodies.set(
          url,
          (await input.clone().json()) as Record<string, unknown>,
        );
      }

      return new Response(JSON.stringify(toDraftResponse(makeApiDraft())), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    const result = await saveDraft("proj-1", {
      filePath: "docs/guide.md",
      title: "guide.md",
      content: "# Draft content",
      originalContent: "# Original content",
    });

    const createCall = mockFetch.mock.calls.find((call: [RequestInfo | URL]) =>
      extractFetchUrl(call[0]).includes("/rpc/corpus.createDraft"),
    );
    expect(createCall).toBeDefined();
    expect(requestBodies.get(extractFetchUrl(createCall?.[0]))).toEqual({
      author_type: "human",
      content: "# Draft content",
      file_path: "docs/guide.md",
      operation: "update",
      original_content: "# Original content",
      source: "dashboard",
      title: "guide.md",
    });
    expect(result.id).toBe("draft-1");
  });

  it("updates an existing draft when draft id is provided", async () => {
    const requestBodies = new Map<string, Record<string, unknown>>();
    mockFetch.mockImplementationOnce(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);
      if (input instanceof Request) {
        requestBodies.set(
          url,
          (await input.clone().json()) as Record<string, unknown>,
        );
      }

      return new Response(JSON.stringify(toDraftResponse(makeApiDraft())), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    const result = await saveDraft("proj-1", {
      draftId: "draft-1",
      filePath: "docs/guide.md",
      content: "# Updated draft content",
    });

    const updateCall = mockFetch.mock.calls.find((call: [RequestInfo | URL]) =>
      extractFetchUrl(call[0]).includes("/rpc/corpus.updateDraft"),
    );
    expect(updateCall).toBeDefined();
    expect(requestBodies.get(extractFetchUrl(updateCall?.[0]))).toEqual({
      id: "draft-1",
      content: "# Updated draft content",
    });
    expect(result.id).toBe("draft-1");
  });
});
