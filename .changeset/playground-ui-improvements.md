---
"dashboard": minor
---

Upgrade to AI SDK 5 and improve playground functionality

- Upgraded to AI SDK 5 with new chat transport and message handling
- Fixed keyboard shortcuts in playground chat input - Enter now properly submits messages (Shift+Enter for newlines)
- Fixed TextArea component to properly accept and forward event handlers (onKeyDown, onCompositionStart, onCompositionEnd, onPaste)
- Fixed AI SDK 5 compatibility by changing maxTokens to maxOutputTokens in CustomChatTransport
- Fixed Button variant types in EditToolDialog (destructive-secondary, secondary)
- Fixed Input component onChange handler to use value parameter directly
- Fixed type mismatches between ToolsetEntry and Toolset in Playground component
- Added missing Tool type import
