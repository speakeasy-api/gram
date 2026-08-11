import type { Meta, StoryObj } from "@storybook/react-vite";

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from ".";
import { Button } from "@/components/ui/Button";

const meta: Meta<typeof Collapsible> = {
  title: "Design System/Collapsible",
  component: Collapsible,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Collapsible>;

export const Default: Story = {
  render: () => (
    <Collapsible className="w-96">
      <CollapsibleTrigger asChild>
        <Button variant="tertiary">Advanced settings</Button>
      </CollapsibleTrigger>
      <CollapsibleContent className="text-muted-foreground pt-2 text-sm">
        Request timeouts, retries and header passthrough live here.
      </CollapsibleContent>
    </Collapsible>
  ),
};
