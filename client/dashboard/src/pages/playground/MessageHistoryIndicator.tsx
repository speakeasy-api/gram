import { Text } from "@/components/ui/Text";
import { Stack } from "@/components/ui/Stack";

interface MessageHistoryIndicatorProps {
  isNavigating: boolean;
  historyIndex: number;
  totalMessages: number;
}

export function MessageHistoryIndicator({
  isNavigating,
  historyIndex,
  totalMessages,
}: MessageHistoryIndicatorProps): JSX.Element | null {
  if (!isNavigating || totalMessages === 0) {
    return null;
  }

  return (
    <div className="bg-background/80 absolute right-2 bottom-2 z-10 border px-2 py-1 backdrop-blur-sm">
      <Stack direction="horizontal" gap={1} align="center">
        <Text variant="small" muted className="text-xs">
          History: {historyIndex + 1}/{totalMessages}
        </Text>
        <Text variant="small" muted className="text-xs">
          (↑↓ to navigate, Esc to exit)
        </Text>
      </Stack>
    </div>
  );
}
