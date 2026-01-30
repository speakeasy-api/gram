import { useState } from "react";
import { toast } from "sonner";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { LinkIcon, UploadIcon } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { FullWidthUpload } from "@/components/upload";
import { Spinner } from "@/components/ui/spinner";
import { useMutation } from "@tanstack/react-query";
import { useSdkClient } from "@/contexts/Sdk";
import { UploadOpenAPIv3Result } from "@gram/client/models/components";

interface OpenApiSourceInputProps {
  onUpload: (file: File) => void;
  onUrlUpload?: (result: UploadOpenAPIv3Result) => void;
  className?: string;
}

export function OpenApiSourceInput({
  onUpload,
  onUrlUpload,
  className,
}: OpenApiSourceInputProps) {
  const [url, setUrl] = useState("");
  const client = useSdkClient();

  const fetchMutation = useMutation({
    mutationFn: async (urlToFetch: string) => {
      const result = await client.assets.fetchOpenAPIv3FromURL({
        fetchOpenAPIv3FromURLForm2: {
          url: urlToFetch,
        },
      });
      return result;
    },
    onSuccess: (result) => {
      if (onUrlUpload) {
        onUrlUpload(result);
      } else {
        // Fallback: create a placeholder file for compatibility
        const filename = url.split("/").pop() || "openapi.yaml";
        const placeholderFile = new File([], filename, {
          type: "application/yaml",
        });
        onUpload(placeholderFile);
      }
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to fetch URL",
      );
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
      <TabsContent value="upload" className="my-3">
        <FullWidthUpload
          onUpload={onUpload}
          className="max-w-full"
          allowedExtensions={["yaml", "yml", "json"]}
        />
      </TabsContent>
      <TabsContent value="url" className="my-3 flex flex-col justify-center">
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
