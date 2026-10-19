---
"server": patch
---

Under the shadow risk engine mode, realtime enforcement no longer waits for the fine-tuned risk model: the legacy engines answer and the model's verdict is compared when it arrives. Under the llm mode the same applies when no policy enforces from the model and its verdict is only compared.
