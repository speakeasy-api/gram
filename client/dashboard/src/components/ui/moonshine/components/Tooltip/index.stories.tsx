import type { Meta, StoryObj } from "@storybook/react-vite";
import { Copy, ExternalLink, PlusIcon as LucidePlusIcon } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipPortal,
  TooltipProvider,
  TooltipTrigger,
} from ".";
import { Button } from "../Button";
import { IconButton } from "../IconButton";

const meta: Meta<typeof Tooltip> = {
  title: "Moonshine/Tooltip",
  component: Tooltip,
  tags: ["autodocs"],
  parameters: {
    layout: "centered",
    docs: {
      description: {
        component:
          "Tooltip wraps Radix Tooltip primitives with Moonshine styling. Use `TooltipTrigger asChild` with Button or IconButton so the trigger remains the interactive element.",
      },
    },
  },
  decorators: [
    (Story) => (
      <TooltipProvider>
        <Story />
      </TooltipProvider>
    ),
  ],
};

export default meta;

type Story = StoryObj<typeof Tooltip>;

function PlusIcon(props: React.ComponentProps<typeof LucidePlusIcon>) {
  return <LucidePlusIcon {...props} />;
}

export const Default: Story = {
  args: {
    children: [
      <TooltipTrigger asChild key="trigger">
        <Button variant="secondary">Hover me</Button>
      </TooltipTrigger>,
      <TooltipContent key="content">
        <p>Helpful context for this action.</p>
      </TooltipContent>,
    ],
  },
  parameters: {
    docs: {
      description: {
        story:
          "The default pattern uses `TooltipTrigger asChild` so the Button receives pointer and focus events directly.",
      },
    },
  },
};

export const WithIconButton: Story = {
  args: {
    children: [
      <TooltipTrigger asChild key="trigger">
        <IconButton
          icon={<Copy />}
          variant="secondary"
          aria-label="Copy endpoint"
        />
      </TooltipTrigger>,
      <TooltipContent key="content">Copy endpoint</TooltipContent>,
    ],
  },
  parameters: {
    docs: {
      description: {
        story:
          "Icon-only actions should keep a visible tooltip and an accessible `aria-label` on the IconButton.",
      },
    },
  },
};

export const WithButtonIcons: Story = {
  args: {
    delayDuration: 150,
    children: [
      <TooltipTrigger asChild key="trigger">
        <Button variant="primary" size="sm">
          <Button.LeftIcon>
            <PlusIcon />
          </Button.LeftIcon>
          <Button.Text>Create</Button.Text>
          <Button.RightIcon>
            <ExternalLink />
          </Button.RightIcon>
        </Button>
      </TooltipTrigger>,
      <TooltipContent key="content">Create project</TooltipContent>,
    ],
  },
  parameters: {
    docs: {
      description: {
        story:
          "Tooltip works with Button compound components, including left and right icons.",
      },
    },
  },
};

export const Placement: Story = {
  args: {
    defaultOpen: true,
    children: [
      <TooltipTrigger asChild key="trigger">
        <Button variant="secondary" size="sm">
          Right side
        </Button>
      </TooltipTrigger>,
      <TooltipContent side="right" key="content">
        Appears on the right
      </TooltipContent>,
    ],
  },
  parameters: {
    docs: {
      description: {
        story: "TooltipContent accepts Radix placement props such as `side`.",
      },
    },
  },
};

export const WithPortal: Story = {
  name: "With Portal",
  args: {
    defaultOpen: true,
    children: [
      <TooltipTrigger asChild key="trigger">
        <Button variant="secondary">Portaled tooltip</Button>
      </TooltipTrigger>,
      <TooltipPortal key="portal">
        <TooltipContent side="top" sideOffset={8}>
          Rendered in a portal
        </TooltipContent>
      </TooltipPortal>,
    ],
  },
  parameters: {
    docs: {
      description: {
        story:
          "Use TooltipPortal when the tooltip needs to escape parent stacking or overflow contexts.",
      },
    },
  },
};

export const DelayDuration: Story = {
  args: {
    delayDuration: 700,
    children: [
      <TooltipTrigger asChild key="trigger">
        <Button variant="secondary" size="sm">
          Delayed
        </Button>
      </TooltipTrigger>,
      <TooltipContent key="content">Waits before opening</TooltipContent>,
    ],
  },
  parameters: {
    docs: {
      description: {
        story:
          "`delayDuration` can be set per Tooltip when a specific interaction needs to feel faster or slower than the provider default.",
      },
    },
  },
};

export const DisabledTrigger: Story = {
  args: {
    children: [
      <TooltipTrigger asChild key="trigger">
        <span className="inline-flex" tabIndex={0}>
          <Button variant="secondary" disabled>
            Disabled action
          </Button>
        </span>
      </TooltipTrigger>,
      <TooltipContent key="content">
        This action is unavailable until setup finishes.
      </TooltipContent>,
    ],
  },
  parameters: {
    docs: {
      description: {
        story:
          "Disabled buttons do not emit pointer or focus events. Wrap the disabled control in a focusable element when the tooltip needs to explain why it is disabled.",
      },
    },
  },
};
