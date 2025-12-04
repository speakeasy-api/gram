import { useState } from "react";
import { toast } from "sonner";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { LinkIcon, UploadIcon } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { FullWidthUpload } from "@/components/upload";
import { Spinner } from "@/components/ui/spinner";

interface OpenApiSourceInputProps {
  onUpload: (file: File) => void;
  className?: string;
}

export function OpenApiSourceInput({
  onUpload,
  className,
}: OpenApiSourceInputProps) {
  const [url, setUrl] = useState("");
  const [isFetching, setIsFetching] = useState(false);

  const handleFetchFromUrl = async () => {
    const trimmedUrl = url.trim();
    if (!trimmedUrl) {
      toast.error("Please enter a valid URL");
      return;
    }

    try {
      new URL(trimmedUrl);
    } catch {
      toast.error("Please enter a valid URL");
      return;
    }

    setIsFetching(true);
    try {
      const response = await fetch(trimmedUrl);
      if (!response.ok) {
        throw new Error(`Failed to fetch: ${response.status}`);
      }

      const blob = await response.blob();
      const filename = trimmedUrl.split("/").pop() || "openapi.yaml";
      const contentType = blob.type || "application/yaml";
      const file = new File([blob], filename, { type: contentType });

      onUpload(file);
    } catch (error) {
      if (error instanceof TypeError && error.message === "Failed to fetch") {
        toast.error(
          "Unable to fetch from this URL. The server may not allow cross-origin requests. Try downloading the file and uploading it instead.",
        );
      } else {
        toast.error(
          error instanceof Error ? error.message : "Failed to fetch URL",
        );
      }
    } finally {
      setIsFetching(false);
    }
  };

  return (
    <Tabs defaultValue="upload" className={className}>
      <TabsList className="grid w-full grid-cols-2">
        <TabsTrigger value="upload">
          <UploadIcon className="size-4 mr-1.5" />
          Upload
        </TabsTrigger>
        <TabsTrigger value="url">
          <LinkIcon className="size-4 mr-1.5" />
          From URL
        </TabsTrigger>
      </TabsList>
      <TabsContent value="upload" className="mt-4 min-h-[174px]">
        <FullWidthUpload
          onUpload={onUpload}
          className="max-w-full"
          allowedExtensions={["yaml", "yml", "json"]}
        />
      </TabsContent>
      <TabsContent
        value="url"
        className="mt-4 min-h-[174px] flex flex-col justify-center"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            handleFetchFromUrl();
          }}
          className="h-full flex flex-col justify-center"
        >
          <Stack gap={3}>
            <input
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://example.com/openapi.yaml"
              className="w-full px-3 py-2 border rounded-md border-input bg-background text-sm focus:outline-none focus:ring-2 focus:ring-ring"
              disabled={isFetching}
              required
            />
            <Button
              type="submit"
              disabled={!url.trim() || isFetching}
              className="w-full"
            >
              {isFetching && <Spinner className="size-4 mr-2" />}
              {isFetching ? "Loading..." : "Load OpenAPI Spec"}
            </Button>
          </Stack>
        </form>
      </TabsContent>
    </Tabs>
  );
}
