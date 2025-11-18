---
"@gram-ai/functions": minor
---

Gram instances are now composable. This allows some similarity to Hono's grouping pattern, making it possible to split and organize Gram Functions code bases more than before. For example, before:

```typescript
const gram = new Gram({
  envSchema: {
    TRAIN_API_KEY: z.string().describe("API key for the train service"),
    FLIGHT_API_KEY: z.string().describe("API key for the flight service"),
  },
})
  .tool({
    name: "train_book",
    description: "Books a train ticket",
  })
  .tool({
    name: "train_status",
    description: "Gets the status of a train",
  })
  .tool({
    name: "flight_book",
    description: "Books a flight ticket",
  })
  .tool({
    name: "flight_status",
    description: "Gets the status of a flight",
  });
```

And now, with composibility:

```typescript
// train.ts
const trainGram = new Gram({
  envSchema: {
    TRAIN_API_KEY: z.string().describe("API key for the train service"),
  },
})
  .tool({
    name: "train_book",
    description: "Books a train ticket",
  })
  .tool({
    name: "train_status",
    description: "Gets the status of a train",
  });
})

// flight.ts
const flightGram = new Gram({
  envSchema: {
    FLIGHT_API_KEY: z.string().describe("API key for the flight service"),
  },
})
  .tool({
    name: "flight_book",
    description: "Books a flight ticket",
  })
  .tool({
    name: "flight_status",
    description: "Gets the status of a flight",
  });

// index.ts
import { trainGram } from './train'
import { flightGram } from './flight'

const travelGram = new Gram()
  .append(trainGram)
  .append(flightGram);

```
