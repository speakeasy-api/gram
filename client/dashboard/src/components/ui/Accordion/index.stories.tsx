import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from ".";

const meta: Meta<typeof Accordion> = {
  title: "Design System/Accordion",
  component: Accordion,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Accordion>;

export const Single: Story = {
  render: () => (
    <Accordion type="single" collapsible className="w-96">
      <AccordionItem value="tools">
        <AccordionTrigger>What is a toolset?</AccordionTrigger>
        <AccordionContent>
          A named group of tools that an MCP server exposes together.
        </AccordionContent>
      </AccordionItem>
      <AccordionItem value="deployments">
        <AccordionTrigger>What is a deployment?</AccordionTrigger>
        <AccordionContent>
          An immutable snapshot of the sources behind a project.
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  ),
};
