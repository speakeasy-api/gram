import type { Meta, StoryObj } from "@storybook/react-vite";
import { ResourceStory } from "../kit/ResourceStory";
import type { ResourceLocation } from "../kit/ResourceView";
import type { FixtureState } from "../kit/fixtures";
import { catalogDefinition } from "./definition";
import { catalogFixtureData } from "./data.fixtures";

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
        definition: catalogDefinition,
        data: catalogFixtureData({ state, readOnly }),
      }}
      initial={location}
      width={width}
    />
  );
}

const meta: Meta<Args> = {
  title: "Resources/Catalog",
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
export const RecordNotAdded: Story = {
  args: { location: record("com.atlassian/mcp") },
};
export const RecordAdded: Story = {
  args: { location: record("app.linear/mcp") },
};
export const RecordLongValues: Story = {
  args: {
    location: record(
      "com.example.internal/very-long-catalog-reference-for-testing/mcp",
    ),
  },
};
export const RecordReadOnly: Story = {
  args: { location: record("com.atlassian/mcp"), readOnly: true },
};
