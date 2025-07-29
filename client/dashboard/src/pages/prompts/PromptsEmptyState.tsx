
import { EmptyState } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { ToolsetsGraphic } from "../toolsets/ToolsetsEmptyState";

export function PromptsEmptyState({
  onCreatePrompt,
}: {
  onCreatePrompt: () => void;
}) {
  const cta = (
    <Button size="sm" onClick={onCreatePrompt} caps>
      Create a prompt
    </Button>
  );

  return (
    <EmptyState
      heading="No prompts yet"
      description="Gram's prompt builder allows you to easily create and distribute reusable MCP prompts to your users."
      nonEmptyProjectCTA={cta}
      graphic={<ToolsetsGraphic />}
      graphicClassName="scale-90"
    />
  );
}
