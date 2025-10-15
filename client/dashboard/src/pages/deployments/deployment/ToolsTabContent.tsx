import { Heading } from "@/components/ui/heading";
import { ToolList } from "@/components/tool-list";
import { useListTools } from "@/hooks/toolTypes";
import { useMemo } from "react";

export const ToolsTabContent = ({ deploymentId }: { deploymentId: string }) => {
  const { data: tools } = useListTools({
    deploymentId: deploymentId,
  });

  const toolDefinitions = useMemo(() => {
    if (!tools) return [];
    return tools.tools;
  }, [tools]);

  return (
    <div className="w-full max-w-full overflow-hidden">
      <Heading variant="h2" className="mb-6">
        Tools
      </Heading>
      <ToolList tools={toolDefinitions} />
    </div>
  );
};
