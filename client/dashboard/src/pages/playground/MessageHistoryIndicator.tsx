import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";

interface MessageHistoryIndicatorProps {
  isNavigating: boolean;
  historyIndex: number;
  totalMessages: number;
}

export function MessageHistoryIndicator({
  isNavigating,
  historyIndex,
  totalMessages,
}: MessageHistoryIndicatorProps) {
  if (!isNavigating || totalMessages === 0) {
    return null;
  }

  return (
    <div className="bg-background/80 absolute right-2 bottom-2 z-10 rounded-md border px-2 py-1 backdrop-blur-sm">
      <Stack direction="horizontal" gap={1} align="center">
        <Type variant="small" muted className="text-xs">
          History: {historyIndex + 1}/{totalMessages}
        </Type>
        <Type variant="small" muted className="text-xs">
          (↑↓ to navigate, Esc to exit)
        </Type>
      </Stack>
    </div>
  );
}
