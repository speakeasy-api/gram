---
"function-runners": patch
---

Fixed the Gram Functions runner service to detect function ID from the environment using the correct variable name, `GRAM_FUNCTION_ID`, and set it up as logger attribute.
