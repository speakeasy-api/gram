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
    <div className="absolute bottom-2 right-2 bg-background/80 backdrop-blur-sm border rounded-md px-2 py-1 z-10">
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
