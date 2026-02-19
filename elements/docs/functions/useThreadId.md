[**@gram-ai/elements v1.27.3**](../README.md)

***

[@gram-ai/elements](../globals.md) / useThreadId

# Function: useThreadId()

> **useThreadId**(): `object`

Hook to access the current thread ID from the Elements chat.
Returns the thread ID (remoteId) when a thread is active, or null if no thread is loaded.

## Returns

`object`

### threadId

> **threadId**: `string` \| `null`

## Example

```tsx
import { useThreadId } from '@gram-ai/elements'

function ShareButton() {
  const { threadId } = useThreadId()

  const handleShare = () => {
    if (!threadId) return
    const shareUrl = `${window.location.href}?threadId=${threadId}`
    navigator.clipboard.writeText(shareUrl)
  }

  return <button onClick={handleShare} disabled={!threadId}>Share</button>
}
```
