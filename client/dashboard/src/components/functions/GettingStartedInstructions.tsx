import { CodeBlock } from "@/components/code";
import { Text } from "@/components/ui/Text";
import { Stack } from "@/components/ui/Stack";

export function GettingStartedInstructions(): JSX.Element {
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
              <Text small className="text-muted-foreground font-medium">
                {index + 1}
              </Text>
            </div>
            <Text className="font-medium">{item.label}</Text>
          </Stack>
          <CodeBlock language="bash" className="!bg-muted/50 !border-0">
            {item.command}
          </CodeBlock>
        </Stack>
      ))}
    </Stack>
  );
}
