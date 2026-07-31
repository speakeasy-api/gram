import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { SkeletonCode } from "@/components/ui/Skeleton";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import MonacoEditorLazy from "../monaco-editor.lazy";
import { useFetchSourceContent } from "./useFetchSourceContent";

interface ViewSourceDialogContentProps {
  source: {
    name: string;
    slug: string;
    assetId: string;
  } | null;
  isOpenAPI: boolean;
}

export function ViewSourceDialogContent({
  source,
  isOpenAPI,
}: ViewSourceDialogContentProps): JSX.Element {
  const project = useProject();
  const { projectSlug } = useSlugs();

  const {
    data: sourceContent,
    isLoading,
    error,
    refetch,
  } = useFetchSourceContent(source, isOpenAPI, project, projectSlug);

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>
          {source?.name} -{" "}
          {isOpenAPI ? "OpenAPI Specification" : "Tool Manifest"}
        </Dialog.Title>
        {!isOpenAPI && (
          <Text className="text-muted-foreground mt-1 text-sm">
            Shows the tool definitions extracted from the function bundle
          </Text>
        )}
      </Dialog.Header>
      <div className="flex-1 overflow-auto">
        {isLoading ? (
          <SkeletonCode lines={20} />
        ) : error ? (
          <div className="py-8 text-center">
            <Text className="text-destructive">
              {error instanceof Error
                ? error.message
                : "Failed to fetch content"}
            </Text>
            <Button
              variant="secondary"
              size="sm"
              className="mt-4"
              onClick={() => {
                void refetch();
              }}
            >
              Retry
            </Button>
          </div>
        ) : sourceContent ? (
          <MonacoEditorLazy
            value={sourceContent.content}
            language={sourceContent.language}
            height="calc(90vh - 120px)"
          />
        ) : (
          <Text className="text-muted-foreground py-8 text-center">
            No content available
          </Text>
        )}
      </div>
    </>
  );
}
