import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { useListMCPRegistries } from "@gram/client/react-query/listMCPRegistries";
import { useMcpRegistriesClearCacheMutation } from "@gram/client/react-query/mcpRegistriesClearCache";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Spinner } from "@/components/ui/Spinner";
import { Skeleton } from "@/components/ui/Skeleton";
import { useState } from "react";
import { toast } from "sonner";

export function RegistryCacheSection(): JSX.Element {
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
      <Text muted small className="mb-4">
        Clear cached registry data to force a fresh fetch from the registry
        source.
      </Text>

      {isLoading && (
        <div
          role="status"
          aria-label="Loading registries"
          className="space-y-3"
        >
          <Skeleton className="h-[62px] w-full" />
          <Skeleton className="h-[62px] w-full" />
        </div>
      )}

      {!isLoading && registries.length === 0 && (
        <Text muted small>
          No registries configured.
        </Text>
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
                className="justify-between border p-3"
              >
                <div>
                  <Text className="font-medium">{registry.name}</Text>
                  <Text muted small className="font-mono">
                    {registry.url}
                  </Text>
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
                    <Button.LeftIcon>
                      <Spinner className="mr-0 h-3 w-3" />
                    </Button.LeftIcon>
                  )}
                  <Button.Text>Clear Cache</Button.Text>
                </Button>
              </Stack>
            );
          })}
        </div>
      )}
    </div>
  );
}
