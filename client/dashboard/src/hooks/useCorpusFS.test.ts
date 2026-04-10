import { describe, expect, it, vi, beforeEach } from "vitest";
import type { ContextNode } from "@/pages/context/mock-data";

// Mock isomorphic-git
vi.mock("isomorphic-git", () => ({
  default: {
    clone: vi.fn(),
    fetch: vi.fn(),
    log: vi.fn(),
    readdir: vi.fn(),
    readBlob: vi.fn(),
  },
  clone: vi.fn(),
  fetch: vi.fn(),
  log: vi.fn(),
  readdir: vi.fn(),
  readBlob: vi.fn(),
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
  readCorpusTree,
  readCorpusFile,
  parseDocsMcpConfig,
} from "./useCorpusFS";

const mockClone = vi.mocked(git.clone);
const mockFetch = vi.mocked(git.fetch);
const mockReaddir = vi.mocked(zenfs.promises.readdir);
const mockReadFile = vi.mocked(zenfs.promises.readFile);
const mockStat = vi.mocked(zenfs.promises.stat);

beforeEach(() => {
  vi.clearAllMocks();
});

describe("cloneCorpus", () => {
  it("clones repo into ZenFS on mount", async () => {
    mockClone.mockResolvedValueOnce(undefined);

    await cloneCorpus("project-123", "bearer-token");

    expect(mockClone).toHaveBeenCalledWith(
      expect.objectContaining({
        url: expect.stringContaining("/v1/projects/project-123/corpus.git"),
        dir: expect.stringContaining("project-123"),
        depth: 1,
      }),
    );
  });
});

describe("readCorpusTree", () => {
  it("reads file tree from ZenFS", async () => {
    // Mock a directory with files and subdirectories
    mockReaddir.mockImplementation(async (path: string) => {
      if (path.endsWith("project-123")) {
        return ["README.md", "docs"] as unknown as string[];
      }
      if (path.endsWith("docs")) {
        return ["guide.md"] as unknown as string[];
      }
      return [] as unknown as string[];
    });

    mockStat.mockImplementation(async (path: string) => {
      if (path.endsWith("docs")) {
        return { isDirectory: () => true, isFile: () => false, size: 0 } as any;
      }
      return {
        isDirectory: () => false,
        isFile: () => true,
        size: 100,
      } as any;
    });

    const tree = await readCorpusTree("project-123");

    expect(tree).toHaveLength(2);
    const readmeNode = tree.find(
      (n: ContextNode) => n.type === "file" && n.name === "README.md",
    );
    expect(readmeNode).toBeDefined();

    const docsNode = tree.find(
      (n: ContextNode) => n.type === "folder" && n.name === "docs",
    );
    expect(docsNode).toBeDefined();
    if (docsNode?.type === "folder") {
      expect(docsNode.children).toHaveLength(1);
    }
  });
});

describe("readCorpusFile", () => {
  it("reads file content from ZenFS", async () => {
    const content = "# Hello World\n\nThis is a test document.";
    mockReadFile.mockResolvedValueOnce(content as any);

    const result = await readCorpusFile("project-123", "README.md");

    expect(result).toBe(content);
    expect(mockReadFile).toHaveBeenCalledWith(
      expect.stringContaining("README.md"),
      expect.objectContaining({ encoding: "utf-8" }),
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

    const result = await parseDocsMcpConfig("project-123", "");

    expect(result).toEqual({
      version: "1",
      strategy: { chunk_by: "h2", max_chunk_size: 2000 },
    });
  });

  it("handles empty repo gracefully", async () => {
    mockReadFile.mockRejectedValueOnce(new Error("ENOENT"));

    const result = await parseDocsMcpConfig("project-123", "");

    expect(result).toBeNull();
  });
});
