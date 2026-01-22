# PreferredMinThroughput

Preferred minimum throughput (in tokens per second). Can be a number (applies to p50) or an object with percentile-specific cutoffs. Endpoints below the threshold(s) may still be used, but are deprioritized in routing. When using fallback models, this may cause a fallback model to be used instead of the primary model if it meets the threshold.


## Supported Types

### 

```go
preferredMinThroughput := components.CreatePreferredMinThroughputNumber(float64{/* values here */})
```

### PercentileThroughputCutoffs

```go
preferredMinThroughput := components.CreatePreferredMinThroughputPercentileThroughputCutoffs(components.PercentileThroughputCutoffs{/* values here */})
```

### 

```go
preferredMinThroughput := components.CreatePreferredMinThroughputAny(any{/* values here */})
```

