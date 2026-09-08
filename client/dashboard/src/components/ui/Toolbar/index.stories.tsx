import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { Toolbar } from ".";
import { Button } from "@/components/ui/Button";
import type { ViewMode } from "@/components/ui/ViewToggle/use-view-mode";

const meta: Meta<typeof Toolbar> = {
  title: "Design System/Toolbar",
  component: Toolbar,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Toolbar>;

export const ListPageControls: Story = {
  render: function Render() {
    const [search, setSearch] = useState("");
    const [sort, setSort] = useState("updated");
    const [view, setView] = useState<ViewMode>("grid");

    return (
      <Toolbar>
        <Toolbar.Row>
          <Toolbar.Search value={search} onChange={setSearch} />
          <Toolbar.SortBy
            value={sort}
            onChange={setSort}
            options={[
              { value: "updated", label: "Last updated" },
              { value: "name", label: "Name" },
            ]}
          />
          <Toolbar.Actions>
            <Button size="sm">New server</Button>
          </Toolbar.Actions>
          <Toolbar.ViewAs value={view} onChange={setView} />
        </Toolbar.Row>
      </Toolbar>
    );
  },
};
