# Explore query grammar

Explore is a SQL-shaped query language over labeled semantic datasets. A query
chooses exactly one dataset, calculates over its typed fields, and optionally
filters, groups, sorts, and buckets the canonical rows. Physical table names and
source reconciliation are not part of the API contract.

```ebnf
query   = dataset , calculations , [ where ] , [ by ] , [ conditional_by ] ,
          [ sort ] , [ limit ] ;

dataset = "events" | "turn_usage" | "user_usage" ;

calculations = calculation , { calculation } ;
calculation  = "COUNT"
             | "COUNT_DISTINCT" , "(" , dimension , ")"
             | numeric_op , "(" , measure , ")" ;
numeric_op   = "SUM" | "AVG" | "MIN" | "MAX" | "P50" | "P95" | "P99" ;

where       = predicate , { predicate } ;
predicate   = dimension , string_op , value , { value }
            | dimension , "exists"
            | measure , numeric_filter_op , number ;
string_op   = "in" | "not_in" | "contains" ;
numeric_filter_op = "eq" | "neq" | "gt" | "gte" | "lt" | "lte" ;

by      = dimension , { dimension } ;
conditional_by = group_expression , { group_expression } ;
group_expression = name , ":" , predicate ;
sort    = "SORT BY" , canonical_calculation , ( "ASC" | "DESC" ) ;
limit   = "LIMIT" , positive_integer ;

window      = [ from , to ) ;
granularity = seconds ;  (* 0 = whole-range table, otherwise >= 60 *)
```

## Semantic datasets

Dataset IDs are stable query identifiers. Labels and categories are presentation
metadata returned by `explore.meta`.

| ID           | Label      | Category | Canonical grain                          |
| ------------ | ---------- | -------- | ---------------------------------------- |
| `events`     | Events     | `event`  | One observed agent event                 |
| `turn_usage` | Turn usage | `usage`  | One completed `(project, session, turn)` |
| `user_usage` | User usage | `usage`  | One provider user-usage interval/report  |

The usage datasets expose the same wide measures:

- `cost_usd`
- `input_tokens`
- `output_tokens`
- `cache_read_tokens`
- `cache_write_tokens`

These are independent grains. Explore never combines turn usage and provider
user usage in one query or implicitly sums them together.

The usage datasets share one append-only wide measurement store. A semantic
grain discriminator scopes `turn_usage` and `user_usage` before any
canonicalization, filtering, or dimension-value lookup. The discriminator and
physical storage name are not exposed through the API.

Every dataset exposes `request_model` and `response_model` as independent
dimensions. `events` also exposes event dimensions and the numeric
`duration_ms` measure. `turn_usage` exposes turn-scoped dimensions, including
`session_id` and `turn_id`. `user_usage` exposes provider-report dimensions and
intentionally has no session or turn dimensions; its `granularity` dimension
identifies the provider reporting interval.

There is no EAV `metric`/`value` dataset, hidden measurements dataset, `costs`
alias, `tokens` alias, or named-metric query path.

## Source eligibility and identities

Promotion adapters are owned by provider, canonical grain, and authoritative
source channel, not legacy storage. Every approved channel is normalized into
the canonical `otel_logs` raw table before promotion. Separate incremental
materialized views preserve channel-specific eligibility and extraction while
sharing that one source table. Current Anthropic adapters recognize:

| Adapter              | Source channel | Dataset      | Eligibility                                                                        | Observation identity                                                                         |
| -------------------- | -------------- | ------------ | ---------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Anthropic events     | Provider OTel  | `events`     | Claude provider OTel logs                                                          | Source event natural identity, such as request or tool call                                  |
| Anthropic events     | Agent hook     | `events`     | Anthropic or Claude-family hook events                                             | Shared tool/message/session identity when available, otherwise the stable source observation |
| Anthropic turn usage | Provider OTel  | `turn_usage` | Claude `api_request` with both session and turn IDs                                | Turn natural ID plus request `component_id`; `observation_kind=component`                    |
| Anthropic turn usage | Agent hook     | `turn_usage` | Anthropic completed-turn hook with a turn ID and at least one explicit usage field | Turn natural ID plus total `component_id`; `observation_kind=total`                          |
| Anthropic user usage | Provider API   | `user_usage` | Claude usage or cost report                                                        | Shared provider report natural ID                                                            |

An uncorrelated provider request remains an event but is excluded from
`turn_usage`. Provider API cost and token rows for the same report share one
natural ID and merge field by field.

Every physical fact store is append-only. Corrections and overlapping source
observations are appended rather than updated in place.

## Nullability, absence, and explicit zero

Every normalized source field is nullable. Nullability represents source
presence independently of value:

- `NULL`: the source did not report the field;
- present field with value zero: the source explicitly reported zero;
- present string field with an empty value: the source explicitly reported an
  empty value.

Promotion adapters test source-field presence before conversion and emit
`NULL` only for absent fields. They do not convert absence to zero or an empty
string. Presence metadata used by an upstream source is not copied into the
measurement fact. Authority only considers non-null observations for each
field. Therefore a higher-authority explicit zero wins over a lower-authority
non-zero value.
Canonical rows remain nullable through source reconciliation and turn rollup so:

- numeric filters do not treat an absent measure as zero;
- `AVG`, `MIN`, `MAX`, and quantiles exclude absent measures but include
  explicit zero;
- `SUM` reads the direct canonical wide column, where absent values contribute
  no input and explicit zeros remain valid observations.

Query results coalesce empty or non-finite aggregates to zero, and absent
grouping dimensions to an empty string, only at the final API result boundary.

## Standard canonicalization

`events` and `user_usage` use standard field-wise authority.

1. Scope the physical scan to the selected semantic dataset, then bound it by
   project and time.
2. Group observations by `(project_id, natural_id)`.
3. Resolve `occurred_at` with source authority and the deterministic tie-break.
4. Resolve every field independently with:

   ```text
   (field source authority, observed_at, src_event_id)
   ```

   Only observations where `isNotNull(field)` is true are eligible.

5. Apply the exact time window and all user filters to the resulting canonical
   rows.

This allows partial provider cost and token reports to form one logical
`user_usage` row without requiring updates or a replacing engine.

## Turn canonicalization

Turn usage has a three-stage path because provider requests are components while
agent hooks report completed-turn totals. Its semantic dataset scope is applied
before the first stage.

### 1. Deduplicate each component

Group by:

```text
(project_id, natural_id, source_channel, observation_kind, component_id)
```

For each field, keep the latest non-null value by `(observed_at, src_event_id)`.
This collapses replayed requests and appended corrections before any sum runs.

### 2. Build one candidate per source

Anthropic provider OTel:

- accept only `observation_kind=component`;
- sum each non-null measure across the canonical request components;
- return `NULL`, not zero, when no canonical component supplied a measure;
- select dimensions from the latest canonical component that supplied each
  dimension.

Anthropic agent hook:

- accept only `observation_kind=total`;
- select the latest canonical completed-turn total;
- preserve its nullability.

Component and total observations are never summed together.

### 3. Apply field authority

Group source candidates by `(project_id, natural_id)` and select every field
independently. Provider OTel request rollups outrank agent-hook totals for the
wide usage measures. Agent totals fill only fields for which no authoritative
provider rollup is present.

The exact time window and every user field filter apply after this final row
exists. Losing source rows cannot affect grouping, calculations, or dimension
suggestions.

## Calculations

| Operation                      | Target    | Semantics                                          |
| ------------------------------ | --------- | -------------------------------------------------- |
| `COUNT`                        | none      | Count canonical rows at the selected dataset grain |
| `COUNT_DISTINCT(x)`            | dimension | Count distinct non-empty canonical values          |
| `SUM(x)`                       | measure   | Sum the direct canonical measure column            |
| `AVG(x)`                       | measure   | Average present canonical measure values           |
| `MIN(x)` / `MAX(x)`            | measure   | Minimum/maximum present value                      |
| `P50(x)` / `P95(x)` / `P99(x)` | measure   | Quantile over present values                       |

Every result value is a finite `Float64`. Empty aggregates and non-finite values
normalize to zero.

Duplicate canonical calculations deduplicate; the first occurrence keeps its
position. Canonical names are `COUNT` or `OP(column)`, for example
`SUM(input_tokens)`.

## Filters and grouping

Dimensions support:

- `in`
- `not_in`
- `contains` (case-insensitive substring)
- `exists` (non-empty canonical value)

Measures support:

- `eq`
- `neq`
- `gt`
- `gte`
- `lt`
- `lte`

All filters are conjunctive and run after canonicalization. Numeric filters
also require measure presence, so `eq 0` matches explicit zeros only.

`group_by` contains unique dimensions in result order. `group_expressions`
contains structured named predicates with the same field/operator/value rules as
filters. They are evaluated independently after canonicalization and do not
filter rows. Each expression adds a grouping axis whose value is `"true"` when
the predicate matches and `"false"` otherwise.

The result `group_by` tuple lists ordinary dimensions first, followed by
conditional expression names in request order. Group values are parallel to
that list. For example:

```json
{
  "group_by": ["provider"],
  "group_expressions": [
    {
      "name": "Is Claude",
      "dimension": "response_model",
      "op": "in",
      "values": ["claude"]
    }
  ]
}
```

produces result group tuples such as `["anthropic", "true"]` and
`["anthropic", "false"]`. Multiple conditional expressions are independent
grouping axes rather than conjunctive row filters.

## Time and result semantics

- API windows are half-open `[from, to)`. Internally `to` becomes `to - 1ns`.
- Granularity zero produces one whole-range table. Positive granularity is
  floored at 60 seconds and produces a timeseries.
- Timeseries rows order by bucket then group tuple.
- Table rows may sort by a requested canonical calculation and apply a limit.
- Results return the semantic `dataset` ID, never the physical observation
  table name.

The result shape is:

```text
result = dataset
       , calculations[]   (* canonical names in request order *)
       , group_by[]
       , granularity_seconds
       , rows[]

row    = bucket
       , group[]
       , values            (* canonical calculation -> Float64 *)
```

## Validation

Explore rejects:

1. an unknown dataset, field, operation, or calculation column;
2. a query with no dataset or no calculations;
3. `COUNT` with a column;
4. `COUNT_DISTINCT` over a measure;
5. a numeric operation over a dimension;
6. an unknown or repeated group-by dimension;
7. an empty or repeated conditional group name, or one that collides with an
   ordinary grouped dimension;
8. a filter or conditional-group operator that does not match the field type;
9. a predicate with missing, malformed, or non-finite operands;
10. a sort reference not present in the requested calculations;
11. granularity between zero and 60 seconds;
12. legacy named metrics or EAV dataset IDs.
