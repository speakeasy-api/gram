import { AddButton } from "@/components/add-button";
import { CreateThingCard } from "@/components/create-thing-card";
import { Page } from "@/components/page-layout";
import { ToolsBadge } from "@/components/tools-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import {
  PromptTemplate,
  PromptTemplateKind,
} from "@gram/client/models/components";
import { useTemplates } from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { Outlet } from "react-router";

export function useCustomTools() {
  const { data } = useTemplates();
  return data?.templates.filter(
    (template) => template.kind === PromptTemplateKind.HigherOrderTool
  );
}

export function CustomToolsRoot() {
  return <Outlet />;
}

export default function CustomTools() {
  const customTools = useCustomTools();
  const routes = useRoutes();

  const onNewCustomTool = () => {
    routes.customTools.toolBuilderNew.goTo();
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>
          <AddButton onClick={onNewCustomTool} tooltip="New Custom Tool" />
        </Page.Header.Actions>
      </Page.Header>
      <Page.Body>
        {customTools?.map((template) => {
          return <CustomToolCard key={template.id} template={template} />;
        })}
        <CreateThingCard onClick={onNewCustomTool}>
          + New Custom Tool
        </CreateThingCard>
      </Page.Body>
    </Page>
  );
}

export function CustomToolCard({ template }: { template: PromptTemplate }) {
  const routes = useRoutes();

  let inputsBadge = <Badge variant="secondary">No inputs</Badge>;
  if (template.arguments) {
    const args = JSON.parse(template.arguments);
    const tooltipContent = (
      <Stack gap={1} className="max-h-[300px] overflow-y-auto">
        {Object.keys(args.properties).map((key: string, i: number) => {
          return <p key={i}>{key}</p>;
        })}
      </Stack>
    );
    const numInputs = Object.keys(args.properties).length;
    inputsBadge = (
      <Badge
        variant="secondary"
        tooltip={numInputs > 0 ? tooltipContent : undefined}
      >
        {numInputs} input{numInputs === 1 ? "" : "s"}
      </Badge>
    );
  }

  return (
    <Card>
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Card.Title className="normal-case">{template.name}</Card.Title>
          <Stack direction="horizontal" gap={2}>
            {inputsBadge}
            <ToolsBadge toolNames={template.toolsHint} />
          </Stack>
        </Stack>
        <Stack direction="horizontal" gap={3} justify={"space-between"}>
          {template.description ? (
            <Card.Description className="max-w-2/3">
              {template.description}
            </Card.Description>
          ) : null}
          <Type variant="body" muted className="text-sm italic">
            {"Updated "}
            <HumanizeDateTime date={new Date(template.updatedAt)} />
          </Type>
        </Stack>
      </Card.Header>
      <Card.Content>
        <routes.customTools.toolBuilder.Link params={[template.name]}>
          <Button variant="outline">Edit</Button>
        </routes.customTools.toolBuilder.Link>
      </Card.Content>
    </Card>
  );
}

//TODO use me
// const NewToolDialog = ({
//   open,
//   setOpen,
// }: {
//   open: boolean;
//   setOpen: (open: boolean) => void;
// }) => {
//   const [inProgress, setInProgress] = useState(false);
//   const [purpose, setPurpose] = useState("");

//   const onSubmit = () => {
//     setOpen(false);
//   };

//   return (
//     <Dialog open={open} onOpenChange={setOpen}>
//       <Dialog.Content>
//         <Dialog.Header>
//           <Dialog.Title>
//             <Stack direction="horizontal" gap={2} align="center">
//               <Icon name="wand-sparkles" className="text-muted-foreground" />
//               Agentify
//             </Stack>
//           </Dialog.Title>
//           <Dialog.Description>
//             Turn this chat into a reusable agent
//           </Dialog.Description>
//         </Dialog.Header>
//         <Stack gap={4}>
//           <Stack gap={1}>
//             <Heading variant="h5" className="normal-case font-medium">
//               What language should the agent be written in?
//             </Heading>
//           </Stack>
//           <Stack gap={1}>
//             <Heading variant="h5" className="normal-case font-medium">
//               Build a custom tool
//             </Heading>
//             <TextArea
//               value={purpose}
//               onChange={(value) => setPurpose(value)}
//               disabled={inProgress}
//               placeholder="What should the agent do?"
//               rows={4}
//             />
//           </Stack>
//         </Stack>
//         <Dialog.Footer>
//           <Button variant="ghost" onClick={() => setOpen(false)}>
//             Back
//           </Button>
//           <Button onClick={onSubmit} disabled={!purpose || inProgress}>
//             {inProgress && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
//             {inProgress ? "Generating..." : "Agentify"}
//           </Button>
//         </Dialog.Footer>
//       </Dialog.Content>
//     </Dialog>
//   );
// };
