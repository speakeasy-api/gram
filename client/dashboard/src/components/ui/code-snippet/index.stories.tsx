import { expect, within } from "storybook/test";
import { userEvent } from "storybook/test";
import { CodeSnippet, CodeSnippetProps } from ".";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { fn } from "storybook/test";

const meta: Meta<typeof CodeSnippet> = {
  title: "UI/CodeSnippet",
  component: CodeSnippet,
  tags: ["autodocs"],
  parameters: {
    layout: "centered",
  },
};

export default meta;

type Story = StoryObj<typeof CodeSnippet>;

export const Default: Story = {
  args: {
    code: 'console.log("Hello, world!")',
    language: "javascript",
    copyable: true,
  },
};

export const Python: Story = {
  args: {
    code: 'print("Hello, world!")',
    language: "python",
    copyable: true,
  },
};

export const TypescriptMultiline: Story = {
  args: {
    code: `type User = {
  name: string
  age: number
}`,
    language: "typescript",
    copyable: true,
  },
};

export const TypescriptFunctionMultiline: Story = {
  args: {
    code: `function greet(name: string) {
  return \`Hello, \${name}!\`
}`,
    language: "typescript",
    copyable: true,
  },
};

export const Javascript: Story = {
  args: {
    code: 'console.log("Hello, world!")',
    language: "javascript",
    copyable: true,
  },
};

export const Bash: Story = {
  args: {
    code: 'echo "Hello, world!"',
    language: "bash",
    copyable: true,
  },
};

export const BashWithCustomPromptSymbol: Story = {
  args: {
    code: 'echo "Hello, world!"',
    language: "bash",
    copyable: true,
    promptSymbol: ">",
  },
};

export const Java: Story = {
  args: {
    code: 'System.out.println("Hello, world!");',
    language: "java",
    copyable: true,
  },
};

export const Dotnet: Story = {
  args: {
    code: 'Console.WriteLine("Hello, world!");',
    language: "dotnet",
    copyable: true,
  },
};

export const Go: Story = {
  args: {
    code: 'fmt.Println("Hello, world!")',
    language: "go",
    copyable: true,
  },
};

export const Json: Story = {
  args: {
    code: `{
  "name": "John",
  "age": 30
}`,
    language: "json",
    copyable: true,
  },
};

export const UnsupportedLanguage: Story = {
  args: {
    code: 'console.log("Hello, world!")',
    language: "unsupported",
    copyable: true,
  },
};

/**
 * Shiki supports many languages that Speakeasy does not.
 */
export const SemiSupportedLanguage: Story = {
  args: {
    code: `FROM node:20

WORKDIR /app

COPY package.json .

RUN npm install
`,
    language: "dockerfile",
  },
};

export const NonCopyable: Story = {
  args: {
    code: 'console.log("Hello, world!")',
    language: "javascript",
    copyable: false,
  },
};

export const FontSizeXL: Story = {
  args: {
    code: 'console.log("Hello, world!")',
    language: "javascript",
    copyable: true,
    fontSize: "xl",
  },
};

export const FontSize2XL: Story = {
  args: {
    code: 'console.log("Hello, world!")',
    language: "javascript",
    copyable: true,
    fontSize: "2xl",
  },
};

export const Interactive: Story = {
  args: {
    code: 'console.log("Hello, world!")',
    language: "javascript",
    copyable: true,
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const codeSnippet = await canvas.findByRole("button", { name: /copy/i });
    await userEvent.click(codeSnippet);
    await expect(navigator.clipboard.readText()).resolves.toBe(
      'console.log("Hello, world!")',
    );
  },
};

export const WithOnSelectOrCopy: Story = {
  args: {
    code: 'console.log("Hello, world!")',
    language: "javascript",
    copyable: true,
    onSelectOrCopy: fn<NonNullable<CodeSnippetProps["onSelectOrCopy"]>>(),
  },
};

export const Shimmer: Story = {
  args: {
    code: 'console.log("Hello, world!")',
    language: "javascript",
    copyable: true,
    shimmer: true,
  },
};

export const ShowLineNumbers: Story = {
  args: {
    code: `console.log("Hello, world!")
console.log("Hello, world!")
console.log("Hello, world!")
console.log("Hello, world!")
console.log("Hello, world!")
console.log("Hello, world!")`,
    language: "javascript",
    showLineNumbers: true,
  },
};

export const WithSnippetClassName: Story = {
  args: {
    code: `const reallyLongLine = 'this is a really long line of code that will overflow the container'`,
    language: "javascript",
    copyable: true,
    showLineNumbers: true,
    snippetClassName: "whitespace-pre-line max-w-lg",
  },
};
