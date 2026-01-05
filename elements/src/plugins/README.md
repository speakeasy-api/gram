# Plugin Development Guide

This guide will help you create custom plugins for the Gram Elements library.

## What are Plugins?

Plugins enable you to add custom rendering capabilities to the Gram Elements library. They allow you to transform markdown code blocks with specific language identifiers into rich, interactive components.

The typical plugin workflow is:

1. **Extend System Prompt**: The plugin adds instructions to the system prompt, telling the LLM how to return data in a specific format
2. **LLM Responds**: The LLM returns a code fence marked with your plugin's language identifier
3. **Custom Rendering**: Your plugin's custom component renders the code block content

## Plugin Interface

A plugin is defined by the following TypeScript interface:

```typescript
interface Plugin {
  // The language identifier for the code fence (e.g., "vega", "mermaid", "d3")
  language: string

  // Instructions for the LLM on how to use this plugin
  prompt: string

  // Your custom React component that renders the code block
  SyntaxHighlighter: ComponentType<SyntaxHighlighterProps>

  // Optional: Custom header component for the code block
  CodeHeader?: ComponentType<CodeHeaderProps> | null

  // Optional: Whether to override existing plugins with the same language
  overrideExisting?: boolean
}
```

## Support

If you need help creating a plugin:

- Check existing plugins for examples
- Review the TypeScript types in `src/types/plugins.ts`
- Open an issue on GitHub
- Join our community Discord

---

Happy plugin building! 🚀
