import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { MoreActions, type Action } from "@/components/ui/more-actions";
import { Type } from "@/components/ui/type";

const meta: Meta<typeof MoreActions> = {
  title: "UI/MoreActions",
  component: MoreActions,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof MoreActions>;

function MoreActionsWithLog({ actions }: { actions: Action[] }) {
  const [lastClicked, setLastClicked] = useState<string | null>(null);

  const wrapped = actions.map((action) => ({
    ...action,
    onClick: () => {
      setLastClicked(action.label);
      action.onClick();
    },
  }));

  return (
    <div className="flex items-center gap-3">
      <MoreActions actions={wrapped} />
      <Type variant="small" muted>
        {lastClicked ? `Last clicked: ${lastClicked}` : "No action clicked yet"}
      </Type>
    </div>
  );
}

const baseActions: Action[] = [
  { icon: "pencil", label: "Rename", onClick: () => {} },
  { icon: "copy", label: "Duplicate", onClick: () => {} },
  { icon: "trash-2", label: "Delete", onClick: () => {}, destructive: true },
];

export const Default: Story = {
  render: () => <MoreActionsWithLog actions={baseActions} />,
};

export const WithTriggerLabel: Story = {
  render: () => (
    <MoreActions
      triggerLabel="Actions"
      actions={baseActions.map((action) => ({ ...action }))}
    />
  ),
};

export const WithDisabledAction: Story = {
  render: () => (
    <MoreActions
      actions={[
        { icon: "pencil", label: "Rename", onClick: () => {} },
        {
          icon: "download",
          label: "Download",
          onClick: () => {},
          disabled: true,
        },
        {
          icon: "trash-2",
          label: "Delete",
          onClick: () => {},
          destructive: true,
        },
      ]}
    />
  ),
};

export const WithoutIcons: Story = {
  render: () => (
    <MoreActions
      actions={[
        { label: "Mark as read", onClick: () => {} },
        { label: "Archive", onClick: () => {} },
      ]}
    />
  ),
};
