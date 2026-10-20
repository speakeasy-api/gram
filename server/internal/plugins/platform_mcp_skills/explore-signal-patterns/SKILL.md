---
name: explore-signal-patterns
description: Explore how a customer-defined signal appears over time and across available attributes in an explicit Speakeasy AI Control Plane project, preserving matching rules, denominators and evidence context.
---

# Explore signal patterns

Use authenticated Speakeasy AI Control Plane tools to answer a bounded question
about captured activity. First successfully call `list_projects` through this
client's own connection and select the exact project requested by the user.

1. Call `describe_analytics_catalog` with that project ID. `signal_metrics`
   describes the entitled signal-measurement contract and supplies current sensor
   names, modes, ordered signal membership and signal names. Availability requires
   Signals Intelligence and project read access; Explore's rollout is independent.
2. Select the sensor answering the user's question and its signal. Shared signal
   identity does not make different sensors equivalent. State the time window and
   explicit match rule: a multi-label probability threshold, the exclusive selected
   signal, or cumulative probability at/above a zero-based ordered level. For an
   exploratory threshold use 0.5 only when stated clearly; it is not a universal
   quality or severity standard. Never threshold or average `expected_index`.
3. Discover labels with `list_analytics_dimension_values`: use `dataset: signals`,
   `signal_scope` containing the sensor and trigger/run, and
   `dimension: attribute_keys`. To discover values, use `dimension: attribute`
   and the exact `attribute` key. `present: false` means absent; present empty
   strings are a different group. Names such as `department`, `model_family`,
   `model_provider` and `model_name` are starting labels, not a closed vocabulary.
4. Call `run_analytics_query` with `dataset: signals`, explicit `from`/`to`,
   `grain: auto` (or an admitted UTC interval) and `signal_query`:
   - `selection`: `sensor_id`, `signal_id` or `minimum_level`, and `threshold`;
   - `trigger`: `stream`, or `manual` with one exact `run_id`;
   - `group_by`: structured `{attribute: key}` or `{field: name}` references;
   - `filters`: ANDed dimension/operator/value objects; `exists` and `missing`
     take no values;
   - optional `groups` and `points` budgets.
     Use this semantic request rather than composed measures/dimensions. If a
     physical read is refused, narrow the window. Coarser output alone does not
     reduce the canonical input scanned.
5. Explain the result using `signal_result.metadata`, `signal_result.summary`,
   `signal_result.groups` and the complete top-level `rows` (joined by `series_id`).
   Distinguish matching messages, exact distinct sessions, share among measured
   answers, `failed`/`not_applicable`/`draft`/`unmeasured` companions,
   and unknown census coverage. Gaps are unknown, not supported zeroes. Report
   partial edge buckets and top-group selection. A session can belong to several
   attribute groups, so distinct-session counts are not additive.
   Terminal event-level failures such as `invalid_evaluation_context` emit no
   sensor outcome. They contribute to unknown census coverage, not to the
   `failed` counter or any other sensor-outcome companion.
6. When definitions are mixed or an interpretation needs inspection, call
   `get_signal_definition` for an exact contributing sensor and fingerprint.
   Historical snapshots may be unavailable; current configuration is not a
   reconstruction of a past measurement. Treat definition, classifier and input
   changes as possible explanations for a trend.

Return the scope, chosen rule, numerator/denominator, supporting measures, query
time and limitations together. These are descriptive observations, not task
counts, time worked, verified agent errors or causal findings. Live reads can
change with corrections and imports; retain the actual result and request when
reproducibility matters. Do not ask for credentials or propose raw SQL access.
