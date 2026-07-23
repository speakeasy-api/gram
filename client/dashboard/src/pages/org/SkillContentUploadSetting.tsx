import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { handleAPIError } from "@/lib/errors";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import {
  invalidateAllProductFeatures,
  useProductFeatures,
} from "@gram/client/react-query/productFeatures.js";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { FileText } from "lucide-react";

export function SkillContentUploadSetting(): JSX.Element | null {
  const queryClient = useQueryClient();
  const { data: features } = useProductFeatures(undefined, undefined, {
    throwOnError: false,
  });
  const mutation = useFeaturesSetMutation({
    onSuccess: async () => {
      await invalidateAllProductFeatures(queryClient);
    },
    onError: (error) => {
      handleAPIError(error, "Failed to update setting");
    },
  });

  if (features?.skillsEnabled !== true) return null;

  return (
    <Stack direction="horizontal" justify="space-between" align="center">
      <Stack gap={1}>
        <Stack direction="horizontal" align="center" gap={2}>
          <FileText className="text-muted-foreground h-4 w-4" />
          <Type variant="body" className="font-medium">
            Upload Skill Content
          </Type>
        </Stack>
        <Type
          variant="body"
          className="text-muted-foreground mr-8 ml-6 max-w-4xl text-sm"
        >
          When enabled, Gram uploads SKILL.md content at activation so captured
          skills can be inspected. When disabled, Gram only receives skill
          names, source details, hashes, users, and hostnames at activation.
        </Type>
      </Stack>
      <RequireScope scope="org:admin" level="component">
        <Switch
          checked={!features.skillCaptureMetadataOnly}
          onCheckedChange={(enabled) =>
            mutation.mutate({
              request: {
                setProductFeatureRequestBody: {
                  featureName: FeatureName.SkillCaptureMetadataOnly,
                  enabled: !enabled,
                },
              },
            })
          }
          disabled={mutation.isPending}
          aria-label="Upload skill content"
        />
      </RequireScope>
    </Stack>
  );
}
