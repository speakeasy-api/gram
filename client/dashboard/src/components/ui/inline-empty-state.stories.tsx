import type { Meta, StoryObj } from "@storybook/react-vite";
import { InboxIcon, SearchXIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";

const meta: Meta<typeof InlineEmptyState> = {
  title: "UI/InlineEmptyState",
  component: InlineEmptyState,
  tags: ["autodocs"],
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof InlineEmptyState>;

export const Default: Story = {
  render: () => (
    <div className="max-w-md">
      <InlineEmptyState
        icon={<InboxIcon />}
        title="No logs yet"
        description="Tool calls will show up here once your agent starts making requests."
      />
    </div>
  ),
};

export const WithAction: Story = {
  render: () => (
    <div className="max-w-md">
      <InlineEmptyState
        icon={<SearchXIcon />}
        title="No matching results"
        description="Try adjusting your filters or search query."
        action={
          <Button variant="tertiary" size="sm">
            <Button.Text>Clear filters</Button.Text>
          </Button>
        }
      />
    </div>
  ),
};

export const TitleOnly: Story = {
  render: () => (
    <div className="max-w-md">
      <InlineEmptyState title="Nothing to show" />
    </div>
  ),
};

export const NoIcon: Story = {
  render: () => (
    <div className="max-w-md">
      <InlineEmptyState
        title="No toolsets configured"
        description="Create a toolset to start grouping tools for your agents."
      />
    </div>
  ),
};
