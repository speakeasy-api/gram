import { CodeBlock } from "@/components/code";
import { SkeletonCode } from "@/components/ui/skeleton";
import { useProject } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { Dialog } from "@speakeasy-api/moonshine";
import { useEffect, useState } from "react";
import { useParams } from "react-router";
import { NamedAsset } from "./SourceCard";

interface ViewAssetDialogContentProps {
  asset: NamedAsset;
}

export function ViewAssetDialogContent({ asset }: ViewAssetDialogContentProps) {
  const { projectSlug } = useParams();
  const project = useProject();
  const [content, setContent] = useState<string>("");
  const [isPending, setIsPending] = useState(true);

  const downloadURL = new URL("/rpc/assets.serveOpenAPIv3", getServerURL());
  downloadURL.searchParams.set("id", asset.id);
  downloadURL.searchParams.set("project_id", project.id);

  useEffect(() => {
    if (!projectSlug) {
      setContent("");
      return;
    }

    setIsPending(true);
    fetch(downloadURL, {
      credentials: "same-origin",
    })
      .then((assetData) => {
        if (!assetData.ok) {
          setContent("");
          setIsPending(false);
          return;
        }
        return assetData.text();
      })
      .then((content) => {
        if (content) {
          setContent(content);
        }
        setIsPending(false);
      })
      .catch(() => {
        setContent("");
        setIsPending(false);
      });
  }, [projectSlug, asset.id]);

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
