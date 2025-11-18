import { CodeBlock } from "@/components/code";
import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";

export function GettingStartedInstructions() {
  const commands = [
    {
      label: (
        <>
          Create a new function project. See gram functions{" "}
          <a
            href="https://www.speakeasy.com/docs/gram/getting-started/typescript"
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary underline cursor-pointer"
          >
            docs
          </a>{" "}
          for more info
        </>
      ),
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
    <div className="p-8">
      <Stack gap={4}>
        {commands.map((item, index) => (
          <Stack key={index} gap={2}>
            <Type small className="font-medium">
              {index + 1}. {item.label}
            </Type>
            <CodeBlock language="bash" preClassName="!bg-transparent">
              {item.command}
            </CodeBlock>
          </Stack>
        ))}
      </Stack>
    </div>
  );
}
