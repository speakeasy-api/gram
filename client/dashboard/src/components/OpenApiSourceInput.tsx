import { useState } from "react";
import { toast } from "sonner";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { LinkIcon, UploadIcon } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { FullWidthUpload } from "@/components/upload";
import { Spinner } from "@/components/ui/spinner";
import { useMutation } from "@tanstack/react-query";

interface OpenApiSourceInputProps {
  onUpload: (file: File) => void;
  className?: string;
}

async function fetchOpenApiFromUrl(url: string): Promise<File> {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`Failed to fetch: ${response.status}`);
  }

  const blob = await response.blob();
  const filename = url.split("/").pop() || "openapi.yaml";
  const contentType = blob.type || "application/yaml";
  return new File([blob], filename, { type: contentType });
}

export function OpenApiSourceInput({
  onUpload,
  className,
}: OpenApiSourceInputProps) {
  const [url, setUrl] = useState("");

  const fetchMutation = useMutation({
    mutationFn: fetchOpenApiFromUrl,
    onSuccess: (file) => {
      onUpload(file);
    },
    onError: (error) => {
      if (error instanceof TypeError && error.message === "Failed to fetch") {
        toast.error(
          "Unable to fetch from this URL. The server may not allow cross-origin requests. Try downloading the file and uploading it instead.",
        );
      } else {
        toast.error(
          error instanceof Error ? error.message : "Failed to fetch URL",
        );
      }
    },
  });

  const handleSubmit = () => {
    const trimmedUrl = url.trim();
    if (!trimmedUrl) {
      toast.error("Please enter a URL");
      return;
    }

    try {
      new URL(trimmedUrl);
    } catch {
      toast.error("Please enter a valid URL");
      return;
    }

    fetchMutation.mutate(trimmedUrl);
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
            handleSubmit();
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
              disabled={fetchMutation.isPending}
              required
            />
            <Button
              type="submit"
              disabled={!url.trim() || fetchMutation.isPending}
              className="w-full"
            >
              {fetchMutation.isPending && <Spinner className="size-4 mr-2" />}
              {fetchMutation.isPending ? "Loading..." : "Load OpenAPI Spec"}
            </Button>
          </Stack>
        </form>
      </TabsContent>
    </Tabs>
  );
}
