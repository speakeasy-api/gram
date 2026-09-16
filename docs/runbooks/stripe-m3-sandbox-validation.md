# Stripe M3 Sandbox Validation

Use this runbook to validate PAYG conversion, metering, invoice allocation, and
subscription loss in a Stripe sandbox. Run it twice from a clean test clock.
Keep the two run IDs and the command output in the internal rollout record.

This runbook never uses production Stripe objects. Do not paste customer,
organization, project, email, or spend data into commits, pull requests, or CI
logs. Use only synthetic values and placeholders such as `<ORG_ID>`.

## What the clocks control

Stripe test clocks advance Stripe objects only. They do not advance Gram's
Postgres clock, Temporal clock, worker schedules, or activity `Now` inputs.
The sandbox portion below validates real Stripe subscription and invoice state.
The deterministic Gram tests use explicit timestamps to validate billing-period
and invoice-allocation transitions without waiting for wall-clock time.

Stripe advances a clock asynchronously. After every advance, poll until its
status is `ready` before inspecting invoices or subscriptions. Test-clock
objects are omitted from unscoped list calls, so retrieve them by clock,
customer, subscription, or exact object ID.

References:

- [Stripe test-clock API workflow](https://docs.stripe.com/billing/testing/test-clocks/api-advanced-usage)
- [Usage-based invoice finalization grace periods](https://docs.stripe.com/billing/subscriptions/usage-based/configure-grace-period)
- [Smart Retries](https://docs.stripe.com/billing/revenue-recovery/smart-retries)

## Prerequisites

1. Use a Stripe sandbox with a test-mode secret key. Never use an `sk_live_` or
   `rk_live_` key.
2. Start the Docker daemon and prepare the local stack. In Cursor Cloud, start
   Docker with `sudo service docker start`; on macOS, use Docker Desktop.

   ```sh
   ./zero --agent
   ```

3. Configure Stripe once for this local worktree; no organization is required.

   ```sh
   mise install stripe
   mise set --prompt --file mise.local.toml STRIPE_API_KEY
   mise run stripe:setup
   ```

   Setup preflights the managed CLI, local target and
   webhook authentication before catalog writes. Only test-mode credentials are
   accepted. It reuses compatible metered prices/products by lookup key and
   billing semantics, not mutable product display names. Conflicting product
   metadata still requires manual resolution; setup does not overwrite it.

   The same API key and signing secret are saved in ignored `mise.local.toml`
   for provisioning, server and listener. An authenticated CLI key can be used
   as a fallback, but **CLI keys expire (typically after 90 days)**. A passing
   check today does not establish durable credentials. After expiry/rotation,
   replace the saved key using
   `mise set --prompt --file mise.local.toml STRIPE_API_KEY`, then rerun setup
   and restart the server, worker and listener. CLI reauthentication alone is
   insufficient: setup prefers the saved `STRIPE_API_KEY` over the CLI key.

   Setup does not read or change organization billing flags, trial state or
   eligibility. Before testing checkout, separately select a **synthetic local**
   organization with the existing `gram-payg-self-serve-billing` feature gate
   enabled and eligible billing/trial state. Keep its identifiers only in your
   shell/internal rollout record (`M3_ORG_ID` / `M3_ORG_SLUG`), not shared logs.
   These are checkout prerequisites, not setup requirements. Trial reset is
   separate work (GRW-92) and is not part of this procedure.

4. In the Stripe sandbox Dashboard, verify these account settings:

   - the PAYG price is monthly, metered, USD, and attached to the configured
     TUM meter;
   - invoice finalization has a 72-hour rule for metered subscription-cycle
     invoices;
   - Smart Retries uses 8 attempts within 2 weeks and cancels the subscription
     after the last failed attempt.

5. Use the worktree-managed listener, not a separate terminal:

   ```sh
   mise run wake
   pitchfork restart server worker
   mise run stripe:listen
   mise run stripe:status
   # When finished:
   mise run pause
   ```

   Setup saves billing configuration; `mise run stripe:listen` validates it,
   registers the daemon in ignored `pitchfork.local.toml` using native Pitchfork,
   and starts the supervisor and listener. Repeating `stripe:listen` overwrites
   old registrations and restarts forwarding with fresh configuration. The
   foreground task `stripe:_listen` is hidden and used only by Pitchfork.
   Listen does not reload server/worker: restart them after saving billing config.
   Setup alone does not register forwarding or remove an existing registration.
   Native `pitchfork start --all-local` (including wake) starts registered
   forwarding; pause stops it with the other worktree daemons. Setup/status
   report missing credentials, server/listener readiness and
   remediation without printing secrets. After key/config changes, restart the
   relevant daemons with fresh mise configuration as directed by status.
   Status deliberately reports the running server/worker configuration as
   **unknown**: it does not inspect their credentials, and a health response
   cannot prove they loaded newly saved settings. `configurationReady` and
   `listenerReady` are not an end-to-end billing readiness claim. `--ready` is
   the supervised listener's readiness check, not proof of billing side effects.

   To opt out, stop the listener and remove its local registration:

   ```sh
   pitchfork stop stripe-listener
   pitchfork daemons remove stripe-listener --local
   ```

   **Listener readiness is not verified webhook delivery.** A connected CLI
   and matching signing secret do not prove that Gram accepted and processed
   an event. After the local mocked checks pass, separately validate a real
   sandbox checkout and its resulting webhook, checking the expected local
   billing transition and successful HTTP delivery. Do not interpret mere
   listener startup (or a synthetic trigger alone) as end-to-end validation.

### Local regression checks (no Stripe mutations)

```sh
mise run test:stripe
```

These mocked tests do not authenticate to Stripe, create checkout sessions,
advance clocks, or change trial state. Real sandbox setup, wake/pause/restart,
credential rotation/expiry, checkout and webhook processing remain manual
validation steps; run them only when authorized.

## Create a clocked Stripe customer

Choose a UTC timestamp before the next midnight and export it as Unix seconds.
Create the clock, then create a customer on that clock. The standard Stripe
test payment method is sandbox-only.

```sh
export M3_CLOCK_START='<UNIX_SECONDS>'

M3_CLOCK_ID="$({
  curl --fail --silent --show-error https://api.stripe.com/v1/test_helpers/test_clocks \
    -u "$STRIPE_API_KEY:" \
    -d frozen_time="$M3_CLOCK_START" \
    -d name='Gram M3 validation'
} | jq -r .id)"
export M3_CLOCK_ID

M3_CUSTOMER_ID="$({
  curl --fail --silent --show-error https://api.stripe.com/v1/customers \
    -u "$STRIPE_API_KEY:" \
    -d test_clock="$M3_CLOCK_ID" \
    -d payment_method=pm_card_visa \
    -d 'invoice_settings[default_payment_method]'=pm_card_visa
} | jq -r .id)"
export M3_CUSTOMER_ID
```

Associate the synthetic organization with this customer before opening
Checkout. This is sandbox setup, not a production application path.

```sh
psql "$GRAM_DATABASE_URL" \
  -v org_id="$M3_ORG_ID" \
  -v customer_id="$M3_CUSTOMER_ID" <<'SQL'
INSERT INTO billing_metadata (organization_id, stripe_customer_id)
VALUES (:'org_id', :'customer_id')
ON CONFLICT (organization_id) DO UPDATE
SET stripe_customer_id = EXCLUDED.stripe_customer_id,
    updated_at = clock_timestamp();
SQL
```

## Convert to PAYG and verify the paid anchor

1. Open `http://localhost:5173/<ORG_SLUG>/billing` as an organization admin.
2. Start PAYG Checkout and complete it with the sandbox card.
3. Wait for `checkout.session.completed` to reach the local webhook listener.
4. Query the durable projection:

   ```sh
   psql "$GRAM_DATABASE_URL" \
     -v org_id="$M3_ORG_ID" \
     -c "SELECT om.gram_account_type, om.whitelisted,
                bm.stripe_customer_id, bm.stripe_subscription_id,
                bm.stripe_billing_cycle_anchor
         FROM organization_metadata om
         JOIN billing_metadata bm ON bm.organization_id = om.id
         WHERE om.id = :'org_id'"
   ```

The organization must be `payg` and admitted, the customer must be unchanged,
and the first paid anchor must be the next `00:00:00Z`. A checkout completed
before that boundary produces only a free Stripe stub; no trial-window or
pre-anchor usage belongs to the paid period.

Retrieve the exact subscription rather than using an unscoped list request:

```sh
export M3_SUBSCRIPTION_ID='<SUBSCRIPTION_ID_FROM_DATABASE>'
curl --fail --silent --show-error \
  "https://api.stripe.com/v1/subscriptions/$M3_SUBSCRIPTION_ID" \
  -u "$STRIPE_API_KEY:" | jq '{id,status,billing_cycle_anchor,test_clock}'
```

Advance the Stripe clock to the paid anchor, poll until ready, then advance one
monthly interval and poll again. Confirm the next subscription-cycle invoice
uses midnight UTC service-period bounds and remains draft under the 72-hour
metered grace rule.

```sh
wait_for_m3_clock() {
  while true; do
    status="$({
      curl --fail --silent --show-error \
        "https://api.stripe.com/v1/test_helpers/test_clocks/$M3_CLOCK_ID" \
        -u "$STRIPE_API_KEY:"
    } | jq -r .status)"
    test "$status" = ready && break
    sleep 2
  done
}

curl --fail --silent --show-error \
  "https://api.stripe.com/v1/test_helpers/test_clocks/$M3_CLOCK_ID/advance" \
  -u "$STRIPE_API_KEY:" \
  -d frozen_time='<NEXT_UTC_MIDNIGHT_UNIX_SECONDS>'
wait_for_m3_clock

curl --fail --silent --show-error \
  "https://api.stripe.com/v1/test_helpers/test_clocks/$M3_CLOCK_ID/advance" \
  -u "$STRIPE_API_KEY:" \
  -d frozen_time='<NEXT_MONTHLY_ANCHOR_UNIX_SECONDS>'
wait_for_m3_clock
```

## Validate Gram's explicit-time billing transitions

Run the deterministic acceptance suites. They seed synthetic Postgres state,
invoke the real activities and webhook handler with explicit timestamps, and
use fake remote boundaries so CI never needs Stripe credentials.

```sh
mise run test:server ./internal/usage \
  -run 'TestM3SubscriptionLossRecheckoutAndStaleReplayLifecycle|TestStripeCheckout' -count=1

mise run test:server ./internal/background/activities \
  -run 'TestSettleStripeInvoiceAllocations' -count=1

mise run test:server ./internal/metering \
  -run 'TestMeterReadingStripeExporter' -count=1
```

Confirm the test output covers all of these checkpoints:

- paid periods start at midnight UTC and exclude the free stub;
- streaming TUM readings retain their reading ID, value, and occurrence time
  when exported to the configured Stripe meter;
- only in-period Other inference spend is allocated; Security inference and
  pre-period spend are excluded;
- exact decimal sums are converted to minor units once per cumulative period;
- positive corrections become a later invoice item and negative corrections
  become a credit note, without duplicate delivery after replay;
- ambiguous allocation writes stay on the same idempotency identity inside
  24 hours and reconcile before a replacement identity is used.

For the sandbox run, compare the same durable records to Stripe by exact object
ID. Do not use unscoped list endpoints.

```sh
psql "$GRAM_DATABASE_URL" -v org_id="$M3_ORG_ID" <<'SQL'
SELECT cycle_start, cycle_end, tum_tokens, finalized_at
FROM billing_cycle_usage
WHERE organization_id = :'org_id'
ORDER BY cycle_start;

SELECT source_kind, source_key, seq, source_snapshot_usd, amount_usd,
       delivery_state, stripe_invoice_item_id, stripe_credit_note_id
FROM stripe_invoice_allocations
WHERE organization_id = :'org_id'
ORDER BY source_period_start, source_key, seq;
SQL
```

TUM exports use Pub/Sub meter readings, not `stripe_meter_reports` or frozen
billing-cycle baselines. Existing TUM carry allocations remain eligible for
settlement. Initial OpenRouter allocation cents plus signed carry cents must
equal the exact final cumulative Other inference spend cents. Every confirmed
allocation external ID must resolve to the same customer, invoice period,
currency, and amount in Stripe.

Enable Stripe exports in `gram streams` with
`GRAM_STRIPE_METER_EVENT_EXPORT_ENABLED=true` and configure
`STRIPE_METER_EVENT_NAME` for TUM. This export switch controls all streaming
meters; TUM has no separate streaming toggle.

## Validate subscription loss and recovery

Cancel the current sandbox subscription. Wait for
`customer.subscription.deleted`, then verify:

- Gram is `free` and not admitted;
- the stored subscription ID and exact Stripe anchor are cleared, while the
  Stripe customer remains;
- the Other inference key is disabled locally and upstream;
- the Security inference key stays enabled with a $5 monthly cap, while its
  last chosen PAYG cap remains available for restoration.

Use the deterministic lifecycle test above to replay the exact signed event and
confirm receipt, audit, metric, and upstream effects do not repeat. In the
sandbox, deliver a deletion for an old subscription after completing a new
Checkout and confirm the replacement remains active.

Complete Checkout again with the same customer. The PAYG activation event must
cause current-state reconciliation to re-enable the same Other inference key
with a $100 monthly cap and restore the Security inference key to its most
recent chosen cap, or $100 when no chosen cap exists.

The voluntary cancellation proves Gram's terminal event behavior. Separately,
confirm the sandbox Dashboard still has the required Smart Retries end action:
8 attempts within 2 weeks, then cancel. Stripe owns the retry schedule and
customer retry emails; Gram reacts only to the terminal deletion.

## Cleanup and mandatory repetition

Delete the test clock. Stripe also deletes its associated sandbox objects.

```sh
curl --fail --silent --show-error -X DELETE \
  "https://api.stripe.com/v1/test_helpers/test_clocks/$M3_CLOCK_ID" \
  -u "$STRIPE_API_KEY:"
```

Delete the synthetic Gram organization through the normal local administration
path. Do not reuse its billing rows for the second run.

Repeat the complete runbook with a new clock, customer, and synthetic Gram
organization. The second run must require no manual database repair and must
produce the same invariants. File a discrepancy before expanding beyond the
internal sandbox cohort.
