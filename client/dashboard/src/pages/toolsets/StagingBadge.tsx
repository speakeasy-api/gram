import { Badge, Icon, Stack } from "@speakeasy-api/moonshine";

interface StagingBadgeProps {
  visible: boolean;
}

export const StagingBadge = ({ visible }: StagingBadgeProps) => {
  if (!visible) {
    return null;
  }

  return (
    <Badge variant="warning">
      <Stack direction="horizontal" gap={1} align="center">
        <Icon name="git-branch" size="small" />
        Staging
      </Stack>
    </Badge>
  );
};
