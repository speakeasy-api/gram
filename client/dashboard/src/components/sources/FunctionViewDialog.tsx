import { CodeBlock } from "@/components/code";
import { Dialog } from "@speakeasy-api/moonshine";
import { SkeletonCode } from "@/components/ui/skeleton";
import { useProject } from "@/contexts/Auth";
import { cn } from "@/lib/utils";
import { FileCode, Folder, FolderOpen } from "lucide-react";
import { useEffect, useState } from "react";
import { useParams } from "react-router";
import { NamedAsset } from "./SourceCard";
import { useViewFunctionSource } from "@gram/client/react-query";

interface FileNode {
  path: string;
  content: string;
  size: number;
  isBinary: boolean;
}

export function FunctionViewDialog({
  asset,
  open,
  onOpenChange,
}: {
  asset: NamedAsset;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { projectSlug } = useParams();
  const project = useProject();
  const [selectedFile, setSelectedFile] = useState<FileNode | null>(null);

  const { data, isLoading, error } = useViewFunctionSource(
    {
      id: asset.id,
      projectId: project.id,
    },
    {
      enabled: open && !!projectSlug,
    },
  );

  const files = data?.files ?? [];

  // Select the first file when data loads
  useEffect(() => {
    if (files.length > 0 && !selectedFile) {
      setSelectedFile(files[0]);
    }
  }, [files, selectedFile]);

  // Reset selected file when dialog closes
  useEffect(() => {
    if (!open) {
      setSelectedFile(null);
    }
  }, [open]);

  // Detect language from file extension
  const getLanguage = (path: string): string => {
    const ext = path.split(".").pop()?.toLowerCase();
    const languageMap: Record<string, string> = {
      js: "javascript",
      mjs: "javascript",
      ts: "typescript",
      mts: "typescript",
      py: "python",
      json: "json",
      yaml: "yaml",
      yml: "yaml",
      md: "markdown",
      txt: "text",
    };
    return languageMap[ext ?? ""] ?? "text";
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="min-w-[80vw] h-[90vh] flex flex-col">
        <Dialog.Header>
          <Dialog.Title>{asset.name}</Dialog.Title>
        </Dialog.Header>

        <div className="flex-1 flex gap-4 overflow-hidden">
          {/* File Tree */}
          <div className="w-64 border-r overflow-auto">
            <div className="p-2 space-y-1">
              {isLoading ? (
                <div className="space-y-2">
                  {[...Array(5)].map((_, i) => (
                    <div
                      key={i}
                      className="h-6 bg-muted animate-pulse rounded"
                    />
                  ))}
                </div>
              ) : error ? (
                <div className="text-destructive text-sm p-2">
                  Failed to load function source
                </div>
              ) : (
                <FileTree
                  files={files}
                  selectedFile={selectedFile}
                  onSelectFile={setSelectedFile}
                />
              )}
            </div>
          </div>

          {/* Code Viewer */}
          <div className="flex-1 overflow-auto">
            {isLoading ? (
              <SkeletonCode lines={50} />
            ) : selectedFile ? (
              selectedFile.isBinary ? (
                <div className="p-4 text-muted-foreground">
                  <div className="flex items-center gap-2 mb-2">
                    <FileCode className="size-4" />
                    <span className="text-sm">Binary file</span>
                  </div>
                  <div className="text-xs">
                    This file cannot be displayed ({selectedFile.size} bytes)
                  </div>
                </div>
              ) : (
                <CodeBlock
                  language={getLanguage(selectedFile.path)}
                  copyable
                  filename={selectedFile.path}
                >
                  {selectedFile.content}
                </CodeBlock>
              )
            ) : (
              <div className="p-4 text-muted-foreground text-center">
                Select a file to view its contents
              </div>
            )}
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

function FileTree({
  files,
  selectedFile,
  onSelectFile,
}: {
  files: FileNode[];
  selectedFile: FileNode | null;
  onSelectFile: (file: FileNode) => void;
}) {
  // Build directory tree structure
  const tree = buildTree(files);

  return (
    <TreeNode
      node={tree}
      selectedFile={selectedFile}
      onSelectFile={onSelectFile}
      level={0}
    />
  );
}

interface TreeNodeData {
  name: string;
  path: string;
  children: Map<string, TreeNodeData>;
  file?: FileNode;
}

function buildTree(files: FileNode[]): TreeNodeData {
  const root: TreeNodeData = {
    name: "",
    path: "",
    children: new Map(),
  };

  for (const file of files) {
    const parts = file.path.split("/");
    let current = root;

    for (let i = 0; i < parts.length; i++) {
      const part = parts[i];
      const isFile = i === parts.length - 1;
      const path = parts.slice(0, i + 1).join("/");

      if (!current.children.has(part)) {
        current.children.set(part, {
          name: part,
          path,
          children: new Map(),
          file: isFile ? file : undefined,
        });
      }

      current = current.children.get(part)!;
    }
  }

  return root;
}

function TreeNode({
  node,
  selectedFile,
  onSelectFile,
  level,
}: {
  node: TreeNodeData;
  selectedFile: FileNode | null;
  onSelectFile: (file: FileNode) => void;
  level: number;
}) {
  const [expanded, setExpanded] = useState(level === 0);
  const isDirectory = node.children.size > 0;
  const isFile = !!node.file;
  const isSelected = selectedFile?.path === node.path;

  // Sort children: directories first, then files
  const sortedChildren = Array.from(node.children.values()).sort((a, b) => {
    const aIsDir = a.children.size > 0;
    const bIsDir = b.children.size > 0;
    if (aIsDir !== bIsDir) return aIsDir ? -1 : 1;
    return a.name.localeCompare(b.name);
  });

  if (level === 0) {
    // Root node, just render children
    return (
      <>
        {sortedChildren.map((child) => (
          <TreeNode
            key={child.path}
            node={child}
            selectedFile={selectedFile}
            onSelectFile={onSelectFile}
            level={level + 1}
          />
        ))}
      </>
    );
  }

  return (
    <div>
      <div
        className={cn(
          "flex items-center gap-1 px-2 py-1 rounded cursor-pointer hover:bg-muted text-sm",
          isSelected && "bg-muted",
        )}
        style={{ paddingLeft: `${level * 12}px` }}
        onClick={() => {
          if (isDirectory) {
            setExpanded(!expanded);
          } else if (isFile && node.file) {
            onSelectFile(node.file);
          }
        }}
      >
        {isDirectory ? (
          expanded ? (
            <FolderOpen className="size-4 shrink-0" />
          ) : (
            <Folder className="size-4 shrink-0" />
          )
        ) : (
          <FileCode className="size-4 shrink-0" />
        )}
        <span className="truncate">{node.name}</span>
      </div>

      {isDirectory && expanded && (
        <div>
          {sortedChildren.map((child) => (
            <TreeNode
              key={child.path}
              node={child}
              selectedFile={selectedFile}
              onSelectFile={onSelectFile}
              level={level + 1}
            />
          ))}
        </div>
      )}
    </div>
  );
}
