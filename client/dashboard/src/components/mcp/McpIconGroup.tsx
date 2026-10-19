import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { Text } from "@/components/ui/Text";
import { Network } from "lucide-react";

export function McpIconGroup({
  serverIds,
}: {
  serverIds: (string | undefined)[];
}): JSX.Element {
  if (serverIds.length === 0) {
    return <Network className="text-muted-foreground h-8 w-8" />;
  }
  const only = serverIds[0];
  if (serverIds.length === 1) {
    return (
      <SourceMcpIcon mcpServerId={only} className="h-8 w-8 object-contain" />
    );
  }

  const overflow = serverIds.length > 4 ? serverIds.length - 3 : 0;
  const shown = serverIds.slice(0, overflow > 0 ? 3 : 4);
  return (
    <div className="grid grid-cols-2 gap-1.5">
      {shown.map((member, index) => (
        <SourceMcpIcon
          key={member ?? index}
          mcpServerId={member}
          className="h-6 w-6 object-contain"
        />
      ))}
      {overflow > 0 && (
        <Text
          muted
          as="div"
          className="flex h-6 w-6 items-center justify-center font-mono text-[10px]"
        >
          +{overflow}
        </Text>
      )}
    </div>
  );
}
