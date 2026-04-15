import { Buffer } from "buffer";
if (typeof globalThis.Buffer === "undefined") {
  globalThis.Buffer = Buffer;
}

import git from "isomorphic-git";
import http from "isomorphic-git/http/web";
import { fs } from "@zenfs/core";
import { getServerURL } from "@/lib/utils";
import type {
  ContextNode,
  DocsMcpConfig,
  FileVersion,
} from "@/pages/context/mock-data";

const ZENFS_BASE = "/corpus-v2";

export type CorpusRepoRef = {
  orgSlug: string;
  projectSlug: string;
  projectId: string;
};

function repoDir(ref: CorpusRepoRef): string {
  return `${ZENFS_BASE}/${encodeURIComponent(ref.orgSlug)}/${encodeURIComponent(ref.projectSlug)}`;
}

export function getCorpusRemoteURL(projectId: string): string {
  return `${getServerURL()}/v1/corpus/git/${projectId}`;
}

async function resetCachedCorpusClone(ref: CorpusRepoRef): Promise<void> {
  await fs.promises.rm(repoDir(ref), { recursive: true, force: true });
}

async function isShallowClone(dir: string): Promise<boolean> {
  try {
    await fs.promises.stat(`${dir}/.git/shallow`);
    return true;
  } catch {
    return false;
  }
}

async function hasDirtyWorktree(dir: string): Promise<boolean> {
  try {
    const matrix = await git.statusMatrix({ fs, dir });
    return matrix.some(([, headStatus, workdirStatus, stageStatus]) => {
      return headStatus !== workdirStatus || workdirStatus !== stageStatus;
    });
  } catch {
    return false;
  }
}

function isLikelyEmptyRepoError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error);
  return (
    message.includes("Could not find") ||
    message.includes("empty") ||
    message.includes("404") ||
    message.includes("HttpError")
  );
}

/**
 * Clone or fetch the corpus git repo into ZenFS.
 * Clones on first visit, fetches on subsequent visits.
 */
export async function cloneCorpus(
  ref: CorpusRepoRef,
  token?: string,
): Promise<void> {
  const dir = repoDir(ref);
  const url = getCorpusRemoteURL(ref.projectId);
  const authOpts = token
    ? { onAuth: () => ({ username: "token", password: token }) }
    : {};

  try {
    // Already cloned — fetch updates and fast-forward
    await fs.promises.stat(`${dir}/.git/HEAD`);
    if (await isShallowClone(dir)) {
      await resetCachedCorpusClone(ref);
      throw new Error("Reset shallow corpus clone");
    }
    if (await hasDirtyWorktree(dir)) {
      return;
    }
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
    return;
  } catch {
    await resetCachedCorpusClone(ref);
  }

  // Fresh clone after cache miss or reset
  try {
    await git.clone({ fs, http, dir, url, ...authOpts });
    const branches = await git.listBranches({ fs, dir });
    const branchRef = branches.includes("main")
      ? "main"
      : branches[0] || "HEAD";
    await git.checkout({ fs, dir, ref: branchRef }).catch(() => {});
  } catch (error) {
    await resetCachedCorpusClone(ref);

    if (isLikelyEmptyRepoError(error)) {
      await fs.promises.mkdir(dir, { recursive: true });
      return;
    }

    throw error;
  }
}

export async function readCorpusTree(
  ref: CorpusRepoRef,
): Promise<ContextNode[]> {
  const dir = repoDir(ref);
  try {
    const headOid = await git.resolveRef({ fs, dir, ref: "HEAD" });
    const commit = await git.readCommit({ fs, dir, oid: headOid });
    return readGitTreeRecursive(dir, commit.commit.tree, "");
  } catch {
    return [];
  }
}

async function readGitTreeRecursive(
  dir: string,
  treeOid: string,
  parentPath: string,
): Promise<ContextNode[]> {
  const tree = await git.readTree({ fs, dir, oid: treeOid });
  const nodes: ContextNode[] = [];

  for (const entry of tree.tree) {
    const filePath = parentPath ? `${parentPath}/${entry.path}` : entry.path;

    if (entry.type === "tree") {
      const children = await readGitTreeRecursive(dir, entry.oid, filePath);
      nodes.push({
        type: "folder",
        name: entry.path,
        children,
        updatedAt: new Date().toISOString(),
      });
      continue;
    }

    let config: DocsMcpConfig | undefined;
    if (entry.path === ".docs-mcp.json") {
      try {
        const configContent = await readCorpusFileByOid(dir, entry.oid);
        config = JSON.parse(configContent) as DocsMcpConfig;
      } catch {
        config = undefined;
      }
    }

    nodes.push({
      type: "file",
      name: entry.path,
      kind:
        entry.path === ".docs-mcp.json"
          ? "mcp-docs-config"
          : entry.path === "SKILL.md"
            ? "skill"
            : "markdown",
      config,
      size: 0,
      updatedAt: new Date().toISOString(),
      versions: [],
    });
  }

  return nodes;
}

async function buildFileVersions(
  dir: string,
  filepath: string,
): Promise<FileVersion[]> {
  const history = await git.log({ fs, dir, filepath });

  const versions = await Promise.all(
    history.map(async (entry, index) => {
      let content: string | undefined;
      let size = 0;

      try {
        const blob = await git.readBlob({
          fs,
          dir,
          oid: entry.oid,
          filepath,
        });
        content = Buffer.from(blob.blob).toString("utf-8");
        size = blob.blob.length;
      } catch {
        // Keep content undefined when the file is not readable in that revision.
      }

      const message = entry.commit.message.trim() || entry.oid.slice(0, 7);

      return {
        version: history.length - index,
        updatedAt: entry.commit.committer.timestamp
          ? new Date(entry.commit.committer.timestamp * 1000).toISOString()
          : new Date().toISOString(),
        author: entry.commit.author.name || "unknown",
        committer: entry.commit.committer.name || entry.commit.author.name,
        message,
        size,
        content,
        path: filepath,
      } satisfies FileVersion;
    }),
  );

  return versions;
}

async function readCorpusFileByOid(dir: string, oid: string): Promise<string> {
  const blob = await git.readBlob({ fs, dir, oid });
  return Buffer.from(blob.blob).toString("utf-8");
}

export async function readCommittedCorpusFile(
  ref: CorpusRepoRef,
  filePath: string,
): Promise<string> {
  const dir = repoDir(ref);
  const headOid = await git.resolveRef({ fs, dir, ref: "HEAD" });
  const blob = await git.readBlob({
    fs,
    dir,
    oid: headOid,
    filepath: filePath,
  });
  return Buffer.from(blob.blob).toString("utf-8");
}

export async function readCorpusFile(
  ref: CorpusRepoRef,
  filePath: string,
): Promise<string> {
  const dir = repoDir(ref);
  const fullPath = `${dir}/${filePath}`;
  try {
    const content = await fs.promises.readFile(fullPath, { encoding: "utf-8" });
    return content as string;
  } catch {
    return readCommittedCorpusFile(ref, filePath);
  }
}

export async function readCorpusFileVersions(
  ref: CorpusRepoRef,
  filePath: string,
): Promise<FileVersion[]> {
  const dir = repoDir(ref);
  return buildFileVersions(dir, filePath);
}

export async function readCorpusDirtyPaths(
  ref: CorpusRepoRef,
): Promise<string[]> {
  const dir = repoDir(ref);

  try {
    const matrix = await git.statusMatrix({ fs, dir });
    return matrix
      .filter(([, headStatus, workdirStatus, stageStatus]) => {
        // Ignore files absent from the partial ZenFS worktree when the index
        // still matches HEAD. Those are not user edits, just not hydrated yet.
        if (workdirStatus === 0 && stageStatus === headStatus) {
          return false;
        }

        return headStatus !== workdirStatus || workdirStatus !== stageStatus;
      })
      .map(([filepath]) => filepath);
  } catch {
    return [];
  }
}

export async function parseDocsMcpConfig(
  ref: CorpusRepoRef,
  dirPath: string,
): Promise<DocsMcpConfig | null> {
  try {
    const configPath = dirPath ? `${dirPath}/.docs-mcp.json` : ".docs-mcp.json";
    const content = await readCorpusFile(ref, configPath);
    return JSON.parse(content) as DocsMcpConfig;
  } catch {
    return null;
  }
}

function getParentDir(filePath: string): string {
  const segments = filePath.split("/").filter(Boolean);
  segments.pop();
  return segments.join("/");
}

function getFileName(filePath: string): string {
  const segments = filePath.split("/").filter(Boolean);
  return segments[segments.length - 1] ?? filePath;
}

export async function writeCorpusFile(
  ref: CorpusRepoRef,
  filePath: string,
  content: string,
): Promise<void> {
  const dir = repoDir(ref);
  const parentDir = getParentDir(filePath);
  if (parentDir) {
    await fs.promises.mkdir(`${dir}/${parentDir}`, { recursive: true });
  }

  await fs.promises.writeFile(`${dir}/${filePath}`, content, {
    encoding: "utf-8",
  });
}

export async function pushCorpusFile(
  ref: CorpusRepoRef,
  filePath: string,
  params: {
    content: string;
    token?: string;
    authorName?: string;
    authorEmail?: string;
    message?: string;
  },
): Promise<string> {
  const dir = repoDir(ref);
  const authOpts = params.token
    ? { onAuth: () => ({ username: "token", password: params.token! }) }
    : {};

  await writeCorpusFile(ref, filePath, params.content);
  await git.add({ fs, dir, filepath: filePath });

  const sha = await git.commit({
    fs,
    dir,
    message: params.message ?? `Update ${getFileName(filePath)}`,
    author: {
      name: params.authorName ?? "Gram User",
      email: params.authorEmail ?? "corpus@getgram.ai",
    },
  });

  await git.push({
    fs,
    http,
    dir,
    remote: "origin",
    ref: "main",
    ...authOpts,
  });

  return sha;
}
