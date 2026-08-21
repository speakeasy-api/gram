import { RequireScope } from "@/components/require-scope";
import { useOrganization } from "@/contexts/Auth";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { handleAPIError } from "@/lib/errors";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import {
  invalidateAllProductFeatures,
  useProductFeatures,
} from "@gram/client/react-query/productFeatures.js";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { FileText } from "lucide-react";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";

export function SkillContentUploadSetting({
  className,
}: {
  className?: string;
} = {}): JSX.Element | null {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  return (
    <SkillContentUploadSettingInner
      key={organization.id}
      organizationId={organization.id}
      isCurrentOrganization={isCurrentOrganization}
      className={className}
    />
  );
}

function SkillContentUploadSettingInner({
  organizationId,
  isCurrentOrganization,
  className,
}: {
  organizationId: string;
  isCurrentOrganization: () => boolean;
  className?: string;
}): JSX.Element | null {
  const queryClient = useQueryClient();
  const { data: features } = useProductFeatures({ organizationId }, undefined, {
    throwOnError: false,
  });
  const mutation = useFeaturesSetMutation({
    onSuccess: async () => {
      if (!isCurrentOrganization()) return;
      await invalidateAllProductFeatures(queryClient);
    },
    onError: (error) => {
      if (!isCurrentOrganization()) return;
      handleAPIError(error, "Failed to update setting");
    },
  });

  if (!features) return null;

  return (
    <Stack
      direction="horizontal"
      justify="space-between"
      align="center"
      className={className}
    >
      <Stack gap={1}>
        <Stack direction="horizontal" align="center" gap={2}>
          <FileText className="text-muted-foreground h-4 w-4" />
          <Text variant="body" className="font-medium">
            Upload Skill Content
          </Text>
        </Stack>
        <Text
          variant="body"
          className="text-muted-foreground mr-8 ml-6 max-w-4xl text-sm"
        >
          When enabled, Speakeasy uploads SKILL.md content at activation so
          captured skills can be inspected. When disabled, Speakeasy only
          receives skill names, source details, hashes, users, and hostnames at
          activation.
        </Text>
      </Stack>
      <RequireScope scope="org:admin" level="component">
        <Switch
          checked={!features.skillCaptureMetadataOnly}
          onCheckedChange={(enabled) =>
            mutation.mutate({
              request: {
                setProductFeatureRequestBody: {
                  organizationId,
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
