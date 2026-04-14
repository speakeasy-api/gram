import { CodeBlock } from "@/components/code";
import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";

export function GettingStartedInstructions() {
  const commands = [
    {
      label: "Create a new function project",
      command: "npm create @gram-ai/function@latest",
    },
    {
      label: "Build your functions",
      command: "npm run build",
    },
    {
      label: "Deploy your functions",
      command: "npm run push",
    },
  ];

  return (
    <Stack gap={6}>
      {commands.map((item, index) => (
        <Stack key={index} gap={2}>
          <Stack direction="horizontal" gap={3} align="center">
            <div className="bg-muted flex h-6 w-6 shrink-0 items-center justify-center rounded-full">
              <Type small className="text-muted-foreground font-medium">
                {index + 1}
              </Type>
            </div>
            <Type className="font-medium">{item.label}</Type>
          </Stack>
          <CodeBlock
            language="bash"
            className="!bg-muted/50 !rounded-lg !border-0"
          >
            {item.command}
          </CodeBlock>
        </Stack>
      ))}
    </Stack>
  );
}
