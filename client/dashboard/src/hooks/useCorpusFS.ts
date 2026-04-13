import { Buffer } from "buffer";
if (typeof globalThis.Buffer === "undefined") {
  globalThis.Buffer = Buffer;
}

import git from "isomorphic-git";
import http from "isomorphic-git/http/web";
import { fs } from "@zenfs/core";
import { getServerURL } from "@/lib/utils";
import type { ContextNode, DocsMcpConfig } from "@/pages/context/mock-data";

const ZENFS_BASE = "/corpus";

function repoDir(projectId: string): string {
  return `${ZENFS_BASE}/${projectId}`;
}

/**
 * Clone or fetch the corpus git repo into ZenFS.
 * Clones on first visit, fetches on subsequent visits.
 */
export async function cloneCorpus(
  projectId: string,
  token?: string,
): Promise<void> {
  const dir = repoDir(projectId);
  const url = `${getServerURL()}/v1/corpus/git/${projectId}`;
  const authOpts = token
    ? { onAuth: () => ({ username: "token", password: token }) }
    : {};

  try {
    // Already cloned — fetch updates and fast-forward
    await fs.promises.stat(`${dir}/.git/HEAD`);
    await git.fetch({ fs, http, dir, ...authOpts });
    const remoteRefs = await git.listBranches({ fs, dir, remote: "origin" });
    if (remoteRefs.includes("main")) {
      const remoteOid = await git.resolveRef({
        fs,
        dir,
        ref: "refs/remotes/origin/main",
      });
      await git.writeRef({
        fs,
        dir,
        ref: "refs/heads/main",
        value: remoteOid,
        force: true,
      });
      await git.checkout({ fs, dir, ref: "main", force: true });
    }
  } catch {
    // Not yet cloned — fresh clone + checkout
    try {
      await git.clone({ fs, http, dir, url, depth: 1, ...authOpts });
      const branches = await git.listBranches({ fs, dir });
      const ref = branches.includes("main") ? "main" : branches[0] || "HEAD";
      await git.checkout({ fs, dir, ref }).catch(() => {});
    } catch {
      // Empty repo — create dir structure
      try {
        await fs.promises.mkdir(dir, { recursive: true });
      } catch {}
    }
  }
}

export async function readCorpusTree(
  projectId: string,
): Promise<ContextNode[]> {
  const dir = repoDir(projectId);
  return readDirRecursive(dir);
}

async function readDirRecursive(dirPath: string): Promise<ContextNode[]> {
  let entries: string[];
  try {
    entries = (await fs.promises.readdir(dirPath)) as unknown as string[];
  } catch {
    return [];
  }
  const nodes: ContextNode[] = [];

  for (const name of entries) {
    if (name === ".git") continue;

    const fullPath = `${dirPath}/${name}`;
    try {
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
    } catch {
      // Skip entries we can't stat
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
