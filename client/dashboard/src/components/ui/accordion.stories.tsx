import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";

const meta: Meta<typeof Accordion> = {
  title: "UI/Accordion",
  component: Accordion,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Accordion>;

export const SingleCollapsible: Story = {
  render: () => (
    <Accordion type="single" collapsible defaultValue="item-1" className="w-96">
      <AccordionItem value="item-1">
        <AccordionTrigger>What is Gram?</AccordionTrigger>
        <AccordionContent>
          Gram turns your API into MCP-compatible tools that agents can call.
        </AccordionContent>
      </AccordionItem>
      <AccordionItem value="item-2">
        <AccordionTrigger>How are tools generated?</AccordionTrigger>
        <AccordionContent>
          Tools are generated from your OpenAPI document during a deployment.
        </AccordionContent>
      </AccordionItem>
      <AccordionItem value="item-3">
        <AccordionTrigger>Can I customize tool names?</AccordionTrigger>
        <AccordionContent>
          Yes, tool names and descriptions can be overridden per toolset.
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  ),
};

export const Multiple: Story = {
  render: () => (
    <Accordion
      type="multiple"
      defaultValue={["item-1", "item-2"]}
      className="w-96"
    >
      <AccordionItem value="item-1">
        <AccordionTrigger>Deployments</AccordionTrigger>
        <AccordionContent>
          Each deployment snapshots your OpenAPI documents and generated tools.
        </AccordionContent>
      </AccordionItem>
      <AccordionItem value="item-2">
        <AccordionTrigger>Toolsets</AccordionTrigger>
        <AccordionContent>
          Toolsets group tools together for a specific agent use case.
        </AccordionContent>
      </AccordionItem>
      <AccordionItem value="item-3">
        <AccordionTrigger>Environments</AccordionTrigger>
        <AccordionContent>
          Environments hold the credentials used when tools are invoked.
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  ),
};

export const DisabledItem: Story = {
  render: () => (
    <Accordion type="single" collapsible defaultValue="item-1" className="w-96">
      <AccordionItem value="item-1">
        <AccordionTrigger>Available section</AccordionTrigger>
        <AccordionContent>This section can be expanded.</AccordionContent>
      </AccordionItem>
      <AccordionItem value="item-2" disabled>
        <AccordionTrigger>Disabled section</AccordionTrigger>
        <AccordionContent>This content is not reachable.</AccordionContent>
      </AccordionItem>
    </Accordion>
  ),
};
