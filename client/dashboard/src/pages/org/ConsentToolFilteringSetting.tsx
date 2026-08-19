import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { handleAPIError } from "@/lib/errors";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import {
  invalidateAllProductFeatures,
  useProductFeatures,
} from "@gram/client/react-query/productFeatures.js";
import { useQueryClient } from "@tanstack/react-query";
import { ListFilter } from "lucide-react";
import { toast } from "sonner";

export function ConsentToolFilteringSetting(): JSX.Element | null {
  const queryClient = useQueryClient();
  const features = useProductFeatures(undefined, undefined, {
    throwOnError: false,
  });
  const mutation = useFeaturesSetMutation({
    onSuccess: async () => {
      await invalidateAllProductFeatures(queryClient);
      toast.success("Tool filtering setting updated");
    },
    onError: (error) => {
      handleAPIError(error, "Failed to update tool filtering setting");
    },
  });

  if (features.isLoading) return null;

  if (features.error || !features.data) {
    return (
      <section
        className="border-border bg-card border p-5"
        role="alert"
        aria-live="polite"
      >
        <Text className="text-destructive" small>
          Couldn&apos;t load the tool filtering setting.
        </Text>
        <Button
          variant="secondary"
          size="sm"
          className="mt-3"
          disabled={features.isFetching}
          onClick={() => void features.refetch()}
        >
          {features.isFetching ? "Retrying…" : "Retry"}
        </Button>
      </section>
    );
  }

  return (
    <section className="border-border bg-card border p-5">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <div className="border-border flex size-9 shrink-0 items-center justify-center border">
            <ListFilter className="text-muted-foreground size-4" />
          </div>
          <div>
            <Text variant="subheading">Tool filtering on consent screens</Text>
            <Text small muted className="mt-1 max-w-3xl">
              Let users narrow a connection to specific tools when they grant
              access on OAuth consent screens. Sessions approved with a
              selection only ever see and call the tools they picked.
            </Text>
          </div>
        </div>
        <RequireScope
          scope="org:admin"
          level="component"
          reason="Only organization admins can change this setting."
        >
          <Switch
            checked={features.data.consentToolFilteringEnabled}
            onCheckedChange={(enabled) =>
              mutation.mutate({
                request: {
                  setProductFeatureRequestBody: {
                    featureName: FeatureName.ConsentToolFiltering,
                    enabled,
                  },
                },
              })
            }
            disabled={mutation.isPending}
            aria-label="Tool filtering on consent screens"
          />
        </RequireScope>
      </div>
    </section>
  );
}
