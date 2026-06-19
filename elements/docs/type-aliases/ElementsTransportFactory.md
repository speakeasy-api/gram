[**@gram-ai/elements v1.37.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / ElementsTransportFactory

# Type Alias: ElementsTransportFactory()

> **ElementsTransportFactory** = (`ctx`) => `ChatTransport`\<`UIMessage`\>

A factory for a ChatTransport. When `ElementsConfig.transport` is a
function, Elements invokes it once inside the provider and passes the live
chat context, letting the transport read the current chat id at send time
without reaching into Elements internals.

## Parameters

### ctx

[`ElementsTransportContext`](../interfaces/ElementsTransportContext.md)

## Returns

`ChatTransport`\<`UIMessage`\>
