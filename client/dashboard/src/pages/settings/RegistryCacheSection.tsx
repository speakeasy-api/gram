import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useListMCPRegistries } from "@gram/client/react-query/listMCPRegistries";
import { useMcpRegistriesClearCacheMutation } from "@gram/client/react-query/mcpRegistriesClearCache";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

export function RegistryCacheSection() {
  const { data, isLoading } = useListMCPRegistries();
  const [clearingId, setClearingId] = useState<string | null>(null);

  const clearCacheMutation = useMcpRegistriesClearCacheMutation({
    onSuccess: () => {
      toast.success("Registry cache cleared");
      setClearingId(null);
    },
    onError: () => {
      toast.error("Failed to clear registry cache");
      setClearingId(null);
    },
  });

  const registries = data?.registries ?? [];

  return (
    <div className="mt-8">
      <Heading variant="h5" className="mb-2">
        MCP Registry Cache
      </Heading>
      <Type muted small className="mb-4">
        Clear cached registry data to force a fresh fetch from the registry
        source.
      </Type>

      {isLoading && (
        <div className="flex items-center gap-2 text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          <Type muted small>
            Loading registries…
          </Type>
        </div>
      )}

      {!isLoading && registries.length === 0 && (
        <Type muted small>
          No registries configured.
        </Type>
      )}

      {registries.length > 0 && (
        <div className="space-y-3">
          {registries.map((registry) => {
            const isClearing = clearingId === registry.id;
            return (
              <Stack
                key={registry.id}
                direction="horizontal"
                align="center"
                className="justify-between rounded-md border p-3"
              >
                <div>
                  <Type className="font-medium">{registry.name}</Type>
                  <Type muted small className="font-mono">
                    {registry.url}
                  </Type>
                </div>
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={isClearing}
                  onClick={() => {
                    setClearingId(registry.id);
                    clearCacheMutation.mutate({
                      request: { registryId: registry.id },
                    });
                  }}
                >
                  {isClearing && (
                    <Loader2 className="mr-2 h-3 w-3 animate-spin" />
                  )}
                  Clear Cache
                </Button>
              </Stack>
            );
          })}
        </div>
      )}
    </div>
  );
}
