import { Heading } from "@/components/ui/heading";
import { ToolsList } from "../ToolsList";

export const ToolsTabContent = ({ deploymentId }: { deploymentId: string }) => {
  return (
    <>
      <Heading variant="h2" className="mb-6">
        Tools
      </Heading>
      <ToolsList deploymentId={deploymentId} />
    </>
  );
};
