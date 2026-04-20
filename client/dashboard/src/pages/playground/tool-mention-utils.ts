export interface Tool {
  id: string;
  name: string;
  description?: string;
  type: "http" | "prompt";
  httpMethod?: string;
  path?: string;
}

export function parseMentionedTools(text: string, tools: Tool[]): string[] {
  // Find all @toolName mentions in the text
  const mentionPattern = /@(\w+)/g;
  const mentions: string[] = [];
  let match;

  while ((match = mentionPattern.exec(text)) !== null) {
    mentions.push(match[1].toLowerCase());
  }

  // Find tools that match the mentions
  const matchedToolIds = tools
    .filter((tool) => mentions.includes(tool.name.toLowerCase()))
    .map((tool) => tool.id);

  return [...new Set(matchedToolIds)]; // Remove duplicates
}
