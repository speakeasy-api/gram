# RFCDate

`types.RFCDate` is a wrapper around Date that allows for JSON marshaling a date string formatted as "2006-01-02".

## Usage

```typescript
const d1 = new RFCDate(Date.now());

const d2 = new RFCDate("2019-01-01");
```