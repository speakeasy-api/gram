[**@gram-ai/elements v1.27.3**](../README.md)

***

[@gram-ai/elements](../globals.md) / ShareButton

# Function: ShareButton()

> **ShareButton**(`__namedParameters`): `Element`

A button component for sharing the current chat thread.
Copies a shareable URL to the clipboard when clicked.

## Parameters

### \_\_namedParameters

[`ShareButtonProps`](../interfaces/ShareButtonProps.md)

## Returns

`Element`

## Example

```tsx
import { ShareButton } from '@gram-ai/elements'
import { toast } from 'sonner'

function MyChat() {
  return (
    <ShareButton
      onShare={(result) => {
        if ('url' in result) {
          toast.success('Chat link copied!')
        } else {
          toast.error(result.error.message)
        }
      }}
    />
  )
}
```
