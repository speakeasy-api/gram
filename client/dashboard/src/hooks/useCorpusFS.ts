import type { ContextNode, DocsMcpConfig } from "@/pages/context/mock-data";

export async function cloneCorpus(
  _projectId: string,
  _token: string,
): Promise<void> {
  throw new Error("not implemented");
}

export async function readCorpusTree(
  _projectId: string,
): Promise<ContextNode[]> {
  throw new Error("not implemented");
}

export async function readCorpusFile(
  _projectId: string,
  _filePath: string,
): Promise<string> {
  throw new Error("not implemented");
}

export async function parseDocsMcpConfig(
  _projectId: string,
  _dirPath: string,
): Promise<DocsMcpConfig | null> {
  throw new Error("not implemented");
}
