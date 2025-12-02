import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { Type } from "@/components/ui/type";
import { useSwitchEditingModeMutation } from "@gram/client/react-query";
import { useCallback } from "react";
import { toast } from "sonner";

interface EditingModeSwitcherProps {
  toolsetSlug: string;
  currentMode: string;
  onModeChanged?: () => void;
}

export const EditingModeSwitcher = ({
  toolsetSlug,
  currentMode,
  onModeChanged,
}: EditingModeSwitcherProps) => {
  const switchMode = useSwitchEditingModeMutation();

  const handleSwitch = useCallback(async () => {
    const newMode = currentMode === "iteration" ? "staging" : "iteration";

    try {
      await switchMode.mutateAsync({
        request: {
          slug: toolsetSlug,
          mode: newMode,
        },
      });

      toast.success(`Switched to ${newMode} mode`, {
        description:
          newMode === "staging"
            ? "Changes will now require explicit releases"
            : "Changes will now be published immediately",
      });

      onModeChanged?.();
    } catch (error) {
      toast.error("Failed to switch editing mode", {
        description: error instanceof Error ? error.message : "Unknown error",
      });
    }
  }, [toolsetSlug, currentMode, switchMode, onModeChanged]);

  const isStaging = currentMode === "staging";

  return (
    <Stack direction="horizontal" gap={2} align="center">
      <Stack direction="horizontal" gap={1} align="center">
        <Icon name={isStaging ? "git-branch" : "zap"} size="small" />
        <Type variant="label" className="text-muted-foreground">
          {isStaging ? "Staging Mode" : "Iteration Mode"}
        </Type>
      </Stack>
      <Button
        variant="secondary"
        size="small"
        onClick={handleSwitch}
        disabled={switchMode.isPending}
      >
        Switch to {isStaging ? "Iteration" : "Staging"}
      </Button>
    </Stack>
  );
};
