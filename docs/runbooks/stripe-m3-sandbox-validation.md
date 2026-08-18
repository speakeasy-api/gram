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
The deterministic Gram tests use explicit timestamps to validate the +48-hour
and +72-hour state transitions without waiting three days.

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

3. Configure and validate the Stripe meter, metered price, and local webhook
   signing secret:

   ```sh
   mise set --prompt --file mise.local.toml STRIPE_API_KEY
   mise run stripe:setup
   ```

4. In the Stripe sandbox Dashboard, verify these account settings:

   - the PAYG price is monthly, metered, USD, and attached to the configured
     TUM meter;
   - invoice finalization has a 72-hour rule for metered subscription-cycle
     invoices;
   - Smart Retries uses 8 attempts within 2 weeks and cancels the subscription
     after the last failed attempt.

5. Restart `server`, then forward sandbox webhooks to Gram in a separate
   terminal. The setup task already saved the listener signing secret to
   ignored `mise.local.toml`.

   ```sh
   pitchfork restart server
   if test "${STRIPE_API_KEY:-}" = unset; then unset STRIPE_API_KEY; fi
   stripe listen --latest --skip-verify \
     --forward-to "$GRAM_SERVER_URL/rpc/stripe.webhook"
   ```

6. Create a synthetic Gram organization through the local dashboard. Record
   its ID and slug only in your shell or the internal rollout record:

   ```sh
   export M3_ORG_ID='<ORG_ID>'
   export M3_ORG_SLUG='<ORG_SLUG>'
   ```

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
mise exec -- go test ./server/internal/usage \
  -run 'TestM3SubscriptionLossRecheckoutAndStaleReplayLifecycle|TestStripeCheckout' -count=1

mise exec -- go test ./server/internal/background/activities \
  -run 'TestM3FreezeObservationAndCarrySettlementLifecycle|TestReportTUMUsageToStripe' -count=1
```

Confirm the test output covers all of these checkpoints:

- paid periods start at midnight UTC and exclude the free stub;
- durable TUM intents use signed database-derived deltas and stable IDs;
- the +48-hour TUM baseline freezes once, the closed-period event timestamp is
  `cycle_end - 1 second`, and a post-freeze difference becomes one signed
  carry-forward allocation after +72 hours;
- only in-period Other inference spend is allocated; Security inference and
  pre-period spend are excluded;
- exact decimal sums are converted to minor units once per cumulative period;
- positive corrections become a later invoice item and negative corrections
  become a credit note, without duplicate delivery after replay;
- ambiguous meter and allocation writes stay on the same idempotency identity
  inside 24 hours and reconcile before a replacement identity is used.

For the sandbox run, compare the same durable records to Stripe by exact object
ID. Do not use unscoped list endpoints.

```sh
psql "$GRAM_DATABASE_URL" -v org_id="$M3_ORG_ID" <<'SQL'
SELECT cycle_start, cycle_end, tum_tokens, billed_tum_tokens,
       billed_frozen_at, finalized_at
FROM billing_cycle_usage
WHERE organization_id = :'org_id'
ORDER BY cycle_start;

SELECT source_kind, source_key, seq, source_snapshot_usd, amount_usd,
       delivery_state, stripe_invoice_item_id, stripe_credit_note_id
FROM stripe_invoice_allocations
WHERE organization_id = :'org_id'
ORDER BY source_period_start, source_key, seq;

SELECT stripe_identifier, delta_tokens, event_timestamp,
       delivery_state, confirmed_at
FROM stripe_meter_reports
WHERE organization_id = :'org_id'
ORDER BY event_timestamp, seq;
SQL
```

The sum of confirmed TUM deltas must equal the frozen billed baseline for the
closed period. Initial OpenRouter allocation cents plus signed carry cents must
equal the exact final cumulative Other inference spend cents. Every confirmed
external ID
must resolve to the same customer, subscription, invoice period, currency, and
amount in Stripe.

## Validate subscription loss and recovery

Cancel the current sandbox subscription. Wait for
`customer.subscription.deleted`, then verify:

- Gram is `free` and not admitted;
- the stored subscription ID and exact Stripe anchor are cleared, while the
  Stripe customer remains;
- the Other inference key is disabled locally and upstream;
- the Security inference key is unchanged.

Use the deterministic lifecycle test above to replay the exact signed event and
confirm receipt, audit, metric, and upstream effects do not repeat. In the
sandbox, deliver a deletion for an old subscription after completing a new
Checkout and confirm the replacement remains active.

Complete Checkout again with the same customer. The PAYG activation event must
cause current-state reconciliation to re-enable the same Other inference key
with its PAYG limit; the Security inference key remains untouched.

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
