import { CodeBlock } from "@/components/code";
import { SkeletonCode } from "@/components/ui/skeleton";
import { useProject } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { Dialog } from "@speakeasy-api/moonshine";
import { useEffect, useState } from "react";
import { NamedAsset } from "./SourceCard";

interface ViewAssetDialogContentProps {
  asset: NamedAsset;
}

function buildDownloadUrl(assetId: string, projectId: string): URL {
  const params = new URLSearchParams({
    id: assetId,
    project_id: projectId,
  });
  return new URL(`/rpc/assets.serveOpenAPIv3?${params}`, getServerURL());
}

export function ViewAssetDialogContent({ asset }: ViewAssetDialogContentProps) {
  const project = useProject();
  const [content, setContent] = useState<string>("");
  const [isPending, setIsPending] = useState(true);

  useEffect(() => {
    const fetchAssetContent = async () => {
      setIsPending(true);
      try {
        const downloadURL = buildDownloadUrl(asset.id, project.id);
        const response = await fetch(downloadURL, {
          credentials: "same-origin",
        });

        if (!response.ok) {
          setContent("");
          return;
        }

        const text = await response.text();
        setContent(text);
      } catch {
        setContent("");
      } finally {
        setIsPending(false);
      }
    };

    fetchAssetContent();
  }, [asset.id, project.id]);

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>{asset.name}</Dialog.Title>
      </Dialog.Header>
      <div className="flex-1 overflow-auto">
        {isPending ? (
          <SkeletonCode lines={20} />
        ) : (
          <CodeBlock language="yaml" copyable>
            {content}
          </CodeBlock>
        )}
      </div>
    </>
  );
}
