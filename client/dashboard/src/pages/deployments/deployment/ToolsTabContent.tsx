import { ToolList } from "@/components/tool-list";
import { useListTools } from "@/hooks/toolTypes";
import { useMemo } from "react";

export const ToolsTabContent = ({
  deploymentId,
}: {
  deploymentId: string;
}): JSX.Element => {
  const { data: tools } = useListTools({
    deploymentId: deploymentId,
  });

  const toolDefinitions = useMemo(() => {
    if (!tools) return [];
    return tools.tools;
  }, [tools]);

  return (
    <div className="w-full max-w-full overflow-hidden">
      <h2 className="text-eyebrow mb-6">Tools</h2>
      <ToolList tools={toolDefinitions} readOnly />
    </div>
  );
};
