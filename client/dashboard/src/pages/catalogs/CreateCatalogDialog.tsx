import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { useListToolsets } from "@gram/client/react-query";
import { Button, Dialog, Input, Stack } from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

interface CreateCatalogDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => void;
}

export function CreateCatalogDialog({
  open,
  onOpenChange,
  onCreated,
}: CreateCatalogDialogProps) {
  const client = useSdkClient();
  const { data: toolsetsData } = useListToolsets();
  const toolsets = toolsetsData?.toolsets ?? [];

  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugManuallyEdited, setSlugManuallyEdited] = useState(false);
  const [selectedToolsetIds, setSelectedToolsetIds] = useState<Set<string>>(
    new Set(),
  );

  const publishMutation = useMutation({
    mutationFn: () =>
      client.mcpRegistries.publish({
        publishRequestBody: {
          name,
          slug,
          toolsetIds: Array.from(selectedToolsetIds),
          visibility: "private",
        },
      }),
    onSuccess: () => {
      toast.success("Catalog created successfully");
      setName("");
      setSlug("");
      setSlugManuallyEdited(false);
      setSelectedToolsetIds(new Set());
      onCreated();
    },
    onError: (err) => {
      toast.error(`Failed to create catalog: ${err.message}`);
    },
  });

  const handleNameChange = (value: string) => {
    setName(value);
    if (!slugManuallyEdited) {
      setSlug(slugify(value));
    }
  };

  const toggleToolset = (id: string) => {
    setSelectedToolsetIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const canSubmit =
    name.trim().length > 0 &&
    slug.trim().length > 0 &&
    selectedToolsetIds.size > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-lg!">
        <Stack direction="vertical" gap={4} className="p-6">
          <div>
            <Heading variant="h5">Create Catalog</Heading>
            <Type small muted>
              Publish a collection of toolsets as an internal MCP registry
              catalog.
            </Type>
          </div>

          <div>
            <label className="text-sm font-medium mb-1 block">Name</label>
            <Input
              placeholder="My Catalog"
              value={name}
              onChange={(e) => handleNameChange(e.target.value)}
            />
          </div>

          <div>
            <label className="text-sm font-medium mb-1 block">Slug</label>
            <Input
              placeholder="my-catalog"
              value={slug}
              onChange={(e) => {
                setSlug(e.target.value);
                setSlugManuallyEdited(true);
              }}
            />
          </div>

          <div>
            <label className="text-sm font-medium mb-2 block">
              Toolsets ({selectedToolsetIds.size} selected)
            </label>
            <div className="max-h-48 overflow-y-auto border rounded-md">
              {toolsets.map((toolset) => (
                <label
                  key={toolset.id}
                  className="flex items-center gap-2 px-3 py-2 hover:bg-muted/50 cursor-pointer border-b last:border-b-0"
                >
                  <input
                    type="checkbox"
                    checked={selectedToolsetIds.has(toolset.id)}
                    onChange={() => toggleToolset(toolset.id)}
                    className="rounded"
                  />
                  <span className="text-sm">{toolset.name}</span>
                  {toolset.description && (
                    <span className="text-xs text-muted-foreground truncate">
                      {toolset.description}
                    </span>
                  )}
                </label>
              ))}
              {toolsets.length === 0 && (
                <div className="px-3 py-4 text-sm text-muted-foreground text-center">
                  No toolsets available
                </div>
              )}
            </div>
          </div>

          <Stack direction="horizontal" gap={2} justify="end">
            <Button
              variant="secondary"
              onClick={() => onOpenChange(false)}
              size="sm"
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              onClick={() => publishMutation.mutate()}
              disabled={!canSubmit || publishMutation.isPending}
              size="sm"
            >
              {publishMutation.isPending && (
                <Loader2 className="w-4 h-4 animate-spin mr-1" />
              )}
              <Button.Text>Create</Button.Text>
            </Button>
          </Stack>
        </Stack>
      </Dialog.Content>
    </Dialog>
  );
}
