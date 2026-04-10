import git from "isomorphic-git";
import http from "isomorphic-git/http/web";
import { fs } from "@zenfs/core";
import type { ContextNode, DocsMcpConfig } from "@/pages/context/mock-data";

const CORPUS_BASE = "/corpus";

function repoDir(projectId: string): string {
  return `${CORPUS_BASE}/${projectId}`;
}

export async function cloneCorpus(
  projectId: string,
  token: string,
): Promise<void> {
  const dir = repoDir(projectId);

  await git.clone({
    fs,
    http,
    dir,
    url: `/v1/projects/${projectId}/corpus.git`,
    depth: 1,
    onAuth: () => ({ username: "token", password: token }),
  });
}

export async function fetchCorpus(
  projectId: string,
  token: string,
): Promise<void> {
  const dir = repoDir(projectId);

  await git.fetch({
    fs,
    http,
    dir,
    onAuth: () => ({ username: "token", password: token }),
  });
}

export async function readCorpusTree(
  projectId: string,
): Promise<ContextNode[]> {
  const dir = repoDir(projectId);
  return readDirRecursive(dir);
}

async function readDirRecursive(dirPath: string): Promise<ContextNode[]> {
  const entries = await fs.promises.readdir(dirPath);
  const nodes: ContextNode[] = [];

  for (const name of entries) {
    if (name === ".git") continue;

    const fullPath = `${dirPath}/${name}`;
    const stat = await fs.promises.stat(fullPath);

    if (stat.isDirectory()) {
      const children = await readDirRecursive(fullPath);
      nodes.push({
        type: "folder",
        name,
        children,
        updatedAt: new Date().toISOString(),
      });
    } else {
      nodes.push({
        type: "file",
        name,
        kind:
          name === ".docs-mcp.json"
            ? "mcp-docs-config"
            : name === "SKILL.md"
              ? "skill"
              : "markdown",
        size: stat.size,
        updatedAt: new Date().toISOString(),
        versions: [],
      });
    }
  }

  return nodes;
}

export async function readCorpusFile(
  projectId: string,
  filePath: string,
): Promise<string> {
  const dir = repoDir(projectId);
  const fullPath = `${dir}/${filePath}`;
  const content = await fs.promises.readFile(fullPath, { encoding: "utf-8" });
  return content as string;
}

export async function parseDocsMcpConfig(
  projectId: string,
  dirPath: string,
): Promise<DocsMcpConfig | null> {
  try {
    const configPath = dirPath ? `${dirPath}/.docs-mcp.json` : ".docs-mcp.json";
    const content = await readCorpusFile(projectId, configPath);
    return JSON.parse(content) as DocsMcpConfig;
  } catch {
    return null;
  }
}
