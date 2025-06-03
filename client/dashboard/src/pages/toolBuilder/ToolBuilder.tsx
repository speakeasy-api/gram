import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { Tool } from "@/lib/toolNames";
import { Column, Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { ToolsTable } from "../toolsets/ToolSelect";

export default function ToolBuilder() {
  const [prompt, setPrompt] = useState("");

  //   const { data: tools } = useListTools();

  const insertColumn: Column<Tool> = {
    header: "Insert",
    key: "insert",
    width: "250px",
    render: (row) => (
      <Button
        variant={"secondary"}
        size={"sm"}
        onClick={() => {
          setPrompt(prompt + `\n\n<Insert ${row.name} />`);
        }}
      >
        <Stack direction={"horizontal"} gap={1}>
          <Type muted mono>
            {"<"}
          </Type>
          <Type>Insert</Type>
          <Type muted mono>
            {"/>"}
          </Type>
        </Stack>
      </Button>
    ),
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Stack direction="vertical" gap={4}>
          <TextArea
            placeholder="Tool Description"
            className="w-full"
            rows={10}
            value={prompt}
            onChange={setPrompt}
          />
          <ToolsTable additionalColumns={[insertColumn]} />
        </Stack>
      </Page.Body>
    </Page>
  );
}
