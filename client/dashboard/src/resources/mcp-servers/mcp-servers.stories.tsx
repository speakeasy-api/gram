import type { Meta, StoryObj } from "@storybook/react-vite";
import { ResourceStory } from "../kit/ResourceStory";
import type { ResourceLocation } from "../kit/ResourceView";
import type { FixtureState } from "../kit/fixtures";
import { mcpServersDefinition } from "./definition";
import { mcpServersFixtureData } from "./data.fixtures";

type Args = {
  location: ResourceLocation;
  state?: FixtureState;
  readOnly?: boolean;
  width?: number;
};

function Render({ location, state, readOnly, width }: Args): JSX.Element {
  return (
    <ResourceStory
      module={{
        definition: mcpServersDefinition,
        data: mcpServersFixtureData({ state, readOnly }),
      }}
      initial={location}
      width={width}
    />
  );
}

const meta: Meta<Args> = {
  title: "Resources/MCP servers",
  render: (args) => <Render {...args} />,
  parameters: { layout: "fullscreen" },
};

export default meta;
type Story = StoryObj<Args>;

const list: ResourceLocation = { view: "list" };
const record = (id: string): ResourceLocation => ({ view: "record", id });

export const List: Story = { args: { location: list } };
export const ListLoading: Story = {
  args: { location: list, state: "loading" },
};
export const ListEmpty: Story = { args: { location: list, state: "empty" } };
export const ListError: Story = { args: { location: list, state: "error" } };
export const ListNarrow: Story = { args: { location: list, width: 400 } };

/** Just created: the setup checklist leads with the next step. */
export const RecordNew: Story = { args: { location: record("srv-linear") } };
/** Off: the checklist's first step turns it on. */
export const RecordOff: Story = { args: { location: record("srv-notion") } };
/** Fully set up: no checklist, quiet header. */
export const RecordHealthy: Story = {
  args: { location: record("srv-datadog") },
};
export const RecordGateway: Story = {
  args: { location: record("gateway:engineering") },
};
export const RecordLongValues: Story = {
  args: { location: record("srv-internal") },
};
/** A member without write access: no write controls, a single note per field. */
export const RecordReadOnly: Story = {
  args: { location: record("srv-linear"), readOnly: true },
};
export const RecordLoading: Story = {
  args: { location: record("srv-linear"), state: "loading" },
};
export const RecordNotFound: Story = {
  args: { location: record("srv-missing") },
};
export const RecordNarrow: Story = {
  args: { location: record("srv-linear"), width: 400 },
};
