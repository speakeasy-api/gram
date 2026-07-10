# RiskSegment

A contiguous run of messages in a windowed view (`risk_only` via `risk_segments`, or `query` via `match_segments`), covering one or more matches plus their surrounding context. Messages for a segment are the entries of `Chat.messages` whose `seq` falls within `[first_seq, last_seq]`.

## Example Usage

```typescript
import { RiskSegment } from "@gram/client/models/components/risksegment.js";

let value: RiskSegment = {
  firstSeq: 187501,
  hasMoreAfter: false,
  hasMoreBefore: false,
  lastSeq: 725456,
};
```

## Fields

| Field           | Type      | Required           | Description                                                                                                             |
| --------------- | --------- | ------------------ | ----------------------------------------------------------------------------------------------------------------------- |
| `firstSeq`      | _number_  | :heavy_check_mark: | The `seq` of the first (oldest) message in this segment.                                                                |
| `hasMoreAfter`  | _boolean_ | :heavy_check_mark: | Whether messages exist after this segment within the generation. Expand with an `after_seq` request using `last_seq`.   |
| `hasMoreBefore` | _boolean_ | :heavy_check_mark: | Whether messages exist before this segment within the generation. Expand with a `before_seq` request using `first_seq`. |
| `lastSeq`       | _number_  | :heavy_check_mark: | The `seq` of the last (newest) message in this segment.                                                                 |
