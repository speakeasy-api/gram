import { describe, expect, it, vi, beforeEach } from "vitest";
import type { ContextNode } from "@/pages/context/mock-data";

// Mock isomorphic-git
vi.mock("isomorphic-git", () => ({
  default: {
    clone: vi.fn(),
    fetch: vi.fn(),
    log: vi.fn(),
    readCommit: vi.fn(),
    readdir: vi.fn(),
    readBlob: vi.fn(),
    readTree: vi.fn(),
    listBranches: vi.fn(),
    resolveRef: vi.fn(),
    writeRef: vi.fn(),
    checkout: vi.fn(),
    statusMatrix: vi.fn(),
    add: vi.fn(),
    commit: vi.fn(),
    push: vi.fn(),
  },
  clone: vi.fn(),
  fetch: vi.fn(),
  log: vi.fn(),
  readCommit: vi.fn(),
  readdir: vi.fn(),
  readBlob: vi.fn(),
  readTree: vi.fn(),
  listBranches: vi.fn(),
  resolveRef: vi.fn(),
  writeRef: vi.fn(),
  checkout: vi.fn(),
  statusMatrix: vi.fn(),
  add: vi.fn(),
  commit: vi.fn(),
  push: vi.fn(),
}));

// Mock @zenfs/core
vi.mock("@zenfs/core", () => ({
  configure: vi.fn(),
  fs: {
    promises: {
      readdir: vi.fn(),
      readFile: vi.fn(),
      stat: vi.fn(),
      access: vi.fn(),
      rm: vi.fn(),
      mkdir: vi.fn(),
      writeFile: vi.fn(),
    },
  },
}));

// Mock @zenfs/dom
vi.mock("@zenfs/dom", () => ({
  IndexedDB: vi.fn(),
}));

import git from "isomorphic-git";
import { fs as zenfs } from "@zenfs/core";
import {
  cloneCorpus,
  pushCorpusFile,
  readCommittedCorpusFile,
  readCorpusDirtyPaths,
  readCorpusTree,
  readCorpusFile,
  readCorpusFileVersions,
  parseDocsMcpConfig,
  type CorpusRepoRef,
  writeCorpusFile,
} from "./useCorpusFS";

const mockClone = vi.mocked(git.clone);
const mockFetch = vi.mocked(git.fetch);
const mockLog = vi.mocked(git.log);
const mockReadCommit = vi.mocked(git.readCommit);
const mockReadBlob = vi.mocked(git.readBlob);
const mockReadTree = vi.mocked(git.readTree);
const mockListBranches = vi.mocked(git.listBranches);
const mockResolveRef = vi.mocked(git.resolveRef);
const mockWriteRef = vi.mocked(git.writeRef);
const mockCheckout = vi.mocked(git.checkout);
const mockStatusMatrix = vi.mocked(git.statusMatrix);
const mockAdd = vi.mocked(git.add);
const mockCommit = vi.mocked(git.commit);
const mockPush = vi.mocked(git.push);
const mockReaddir = vi.mocked(zenfs.promises.readdir);
const mockReadFile = vi.mocked(zenfs.promises.readFile);
const mockStat = vi.mocked(zenfs.promises.stat);
const mockRm = vi.mocked(zenfs.promises.rm);
const mockMkdir = vi.mocked(zenfs.promises.mkdir);
const mockWriteFile = vi.mocked(zenfs.promises.writeFile);

const repoRef: CorpusRepoRef = {
  projectId: "project-123",
  orgSlug: "local-dev-org",
  projectSlug: "default",
};

beforeEach(() => {
  vi.clearAllMocks();
  mockListBranches.mockResolvedValue(["main"] as any);
  mockResolveRef.mockResolvedValue("oid-main" as any);
  mockWriteRef.mockResolvedValue(undefined as any);
  mockCheckout.mockResolvedValue(undefined as any);
  mockStatusMatrix.mockResolvedValue([] as any);
  mockAdd.mockResolvedValue(undefined as any);
  mockCommit.mockResolvedValue("commit-sha" as any);
  mockPush.mockResolvedValue(undefined as any);
  mockRm.mockResolvedValue(undefined as any);
  mockMkdir.mockResolvedValue(undefined as any);
  mockWriteFile.mockResolvedValue(undefined as any);
});

describe("cloneCorpus", () => {
  it("clones repo into an org/project keyed ZenFS path", async () => {
    mockStat.mockRejectedValueOnce(new Error("ENOENT"));
    mockClone.mockResolvedValueOnce(undefined);

    await cloneCorpus(repoRef, "bearer-token");

    expect(mockClone).toHaveBeenCalledWith(
      expect.objectContaining({
        url: expect.stringContaining("/v1/corpus/git/project-123"),
        dir: expect.stringContaining("/local-dev-org/default"),
      }),
    );
  });

  it("wipes an existing shallow clone and re-clones without depth", async () => {
    mockStat.mockImplementation(async (path: string) => {
      if (String(path).endsWith(".git/HEAD")) {
        return {} as any;
      }
      if (String(path).endsWith(".git/shallow")) {
        return {} as any;
      }
      throw new Error("ENOENT");
    });
    mockClone.mockResolvedValueOnce(undefined);

    await cloneCorpus(repoRef, "bearer-token");

    expect(mockRm).toHaveBeenCalledWith(
      expect.stringContaining("/local-dev-org/default"),
      expect.objectContaining({ recursive: true, force: true }),
    );
    expect(mockClone).toHaveBeenCalledWith(
      expect.not.objectContaining({ depth: expect.anything() }),
    );
  });

  it("wipes the cached clone and re-clones if fetch fails", async () => {
    mockStat.mockResolvedValueOnce({} as any);
    mockFetch.mockRejectedValueOnce(new Error("corrupt repo"));
    mockClone.mockResolvedValueOnce(undefined);

    await cloneCorpus(repoRef, "bearer-token");

    expect(mockRm).toHaveBeenCalledWith(
      expect.stringContaining("/local-dev-org/default"),
      expect.objectContaining({ recursive: true, force: true }),
    );
    expect(mockClone).toHaveBeenCalledWith(
      expect.objectContaining({
        dir: expect.stringContaining("/local-dev-org/default"),
      }),
    );
  });

  it("keeps local dirty changes instead of fast-forwarding over them", async () => {
    mockStat.mockImplementation(async (path: string) => {
      if (String(path).endsWith(".git/HEAD")) {
        return {} as any;
      }
      throw new Error("ENOENT");
    });
    mockStatusMatrix.mockResolvedValueOnce([["README.md", 1, 2, 0]] as any);

    await cloneCorpus(repoRef, "bearer-token");

    expect(mockFetch).not.toHaveBeenCalled();
    expect(mockClone).not.toHaveBeenCalled();
  });
});

describe("readCorpusTree", () => {
  it("reads file tree metadata without eagerly loading file content or history", async () => {
    mockResolveRef.mockResolvedValueOnce("head-oid" as any);
    mockReadCommit.mockResolvedValueOnce({
      commit: { tree: "root-tree" },
    } as any);
    mockReadTree.mockImplementation(async ({ oid }: any) => {
      if (oid === "root-tree") {
        return {
          tree: [
            {
              path: "README.md",
              type: "blob",
              mode: "100644",
              oid: "readme-blob",
            },
            {
              path: "docs",
              type: "tree",
              mode: "040000",
              oid: "docs-tree",
            },
          ],
        } as any;
      }
      if (oid === "docs-tree") {
        return {
          tree: [
            {
              path: "guide.md",
              type: "blob",
              mode: "100644",
              oid: "guide-blob",
            },
          ],
        } as any;
      }
      return { tree: [] } as any;
    });

    const tree = await readCorpusTree(repoRef);

    expect(tree).toHaveLength(2);
    const readmeNode = tree.find(
      (n: ContextNode) => n.type === "file" && n.name === "README.md",
    );
    expect(readmeNode).toBeDefined();
    if (readmeNode?.type === "file") {
      expect(readmeNode.content).toBeUndefined();
      expect(readmeNode.versions).toEqual([]);
    }

    const docsNode = tree.find(
      (n: ContextNode) => n.type === "folder" && n.name === "docs",
    );
    expect(docsNode).toBeDefined();
    if (docsNode?.type === "folder") {
      expect(docsNode.children).toHaveLength(1);
    }
  });
});

describe("readCommittedCorpusFile", () => {
  it("reads the committed HEAD content for a file", async () => {
    mockResolveRef.mockResolvedValueOnce("head-oid" as any);
    mockReadBlob.mockResolvedValueOnce({
      blob: Buffer.from("# Hello from HEAD"),
    } as any);

    const result = await readCommittedCorpusFile(repoRef, "README.md");

    expect(result).toBe("# Hello from HEAD");
    expect(mockReadBlob).toHaveBeenCalledWith(
      expect.objectContaining({
        oid: "head-oid",
        filepath: "README.md",
      }),
    );
  });
});

describe("readCorpusFile", () => {
  it("reads file content from ZenFS", async () => {
    const content = "# Hello World\n\nThis is a test document.";
    mockReadFile.mockResolvedValueOnce(content as any);

    const result = await readCorpusFile(repoRef, "README.md");

    expect(result).toBe(content);
    expect(mockReadFile).toHaveBeenCalledWith(
      expect.stringContaining("README.md"),
      expect.objectContaining({ encoding: "utf-8" }),
    );
  });

  it("falls back to committed content when the worktree file is not present", async () => {
    mockReadFile.mockRejectedValueOnce(new Error("ENOENT"));
    mockResolveRef.mockResolvedValueOnce("head-oid" as any);
    mockReadBlob.mockResolvedValueOnce({
      blob: Buffer.from("# Hello from HEAD"),
    } as any);

    const result = await readCorpusFile(repoRef, "README.md");

    expect(result).toBe("# Hello from HEAD");
  });
});

describe("readCorpusFileVersions", () => {
  it("loads versions only for the requested file", async () => {
    mockLog.mockResolvedValueOnce([
      {
        oid: "abc123",
        commit: {
          message: "Add README\n",
          author: { name: "alice" },
          committer: { name: "alice", timestamp: 1710000000 },
        },
      },
    ] as any);
    mockReadBlob.mockResolvedValueOnce({
      blob: Buffer.from("# Hello from history"),
    } as any);

    const versions = await readCorpusFileVersions(repoRef, "README.md");

    expect(versions).toHaveLength(1);
    expect(versions[0]?.message).toBe("Add README");
    expect(versions[0]?.content).toBe("# Hello from history");
  });
});

describe("readCorpusDirtyPaths", () => {
  it("returns only files whose worktree differs from HEAD", async () => {
    mockStatusMatrix.mockResolvedValueOnce([
      ["README.md", 1, 2, 0],
      ["docs/guide.md", 1, 1, 1],
      ["draft.md", 0, 2, 0],
      ["sdk/go/README.md", 1, 0, 1],
    ] as any);

    const dirtyPaths = await readCorpusDirtyPaths(repoRef);

    expect(dirtyPaths).toEqual(["README.md", "draft.md"]);
  });
});

describe("writeCorpusFile", () => {
  it("writes file content into the persisted ZenFS repo", async () => {
    await writeCorpusFile(repoRef, "docs/guide.md", "updated guide");

    expect(mockMkdir).toHaveBeenCalledWith(
      expect.stringContaining("/local-dev-org/default/docs"),
      expect.objectContaining({ recursive: true }),
    );
    expect(mockWriteFile).toHaveBeenCalledWith(
      expect.stringContaining("/local-dev-org/default/docs/guide.md"),
      "updated guide",
      expect.objectContaining({ encoding: "utf-8" }),
    );
  });
});

describe("pushCorpusFile", () => {
  it("commits and pushes the current working tree content", async () => {
    const sha = await pushCorpusFile(repoRef, "README.md", {
      content: "# Updated",
      token: "bearer-token",
      authorName: "Alice",
      authorEmail: "alice@example.com",
      message: "Update README",
    });

    expect(sha).toBe("commit-sha");
    expect(mockWriteFile).toHaveBeenCalledWith(
      expect.stringContaining("/local-dev-org/default/README.md"),
      "# Updated",
      expect.objectContaining({ encoding: "utf-8" }),
    );
    expect(mockAdd).toHaveBeenCalledWith(
      expect.objectContaining({ filepath: "README.md" }),
    );
    expect(mockCommit).toHaveBeenCalledWith(
      expect.objectContaining({
        message: "Update README",
        author: { name: "Alice", email: "alice@example.com" },
      }),
    );
    expect(mockPush).toHaveBeenCalledWith(
      expect.objectContaining({
        remote: "origin",
        ref: "main",
      }),
    );
  });
});

describe("parseDocsMcpConfig", () => {
  it("parses .docs-mcp.json from ZenFS", async () => {
    const config = JSON.stringify({
      version: "1",
      strategy: { chunk_by: "h2", max_chunk_size: 2000 },
    });
    mockReadFile.mockResolvedValueOnce(config as any);

    const result = await parseDocsMcpConfig(repoRef, "");

    expect(result).toEqual({
      version: "1",
      strategy: { chunk_by: "h2", max_chunk_size: 2000 },
    });
  });

  it("handles empty repo gracefully", async () => {
    mockReadFile.mockRejectedValueOnce(new Error("ENOENT"));

    const result = await parseDocsMcpConfig(repoRef, "");

    expect(result).toBeNull();
  });
});
