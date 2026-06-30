[**@gram-ai/elements v1.39.0**](../README.md)

***

[@gram-ai/elements](../globals.md) / FrontendTool

# Type Alias: FrontendTool\<TArgs, TResult\>

> **FrontendTool**\<`TArgs`, `TResult`\> = `FC` & `object`

A frontend tool is a tool that is defined by the user and can be used in the chat.

Shape mirrors assistant-ui's `AssistantTool`: an `FC` (rendered with no props
at runtime to register the tool) plus an `unstable_tool` describing the tool
itself. Keeping the FC unparameterised here matches the SDK and allows tools
with different `TArgs`/`TResult` to coexist in a `Record<string, FrontendTool<...>>`.

## Type Declaration

### unstable\_tool

> **unstable\_tool**: `AssistantToolProps`\<`TArgs`, `TResult`\>

## Type Parameters

### TArgs

`TArgs` *extends* `Record`\<`string`, `unknown`\>

### TResult

`TResult`
