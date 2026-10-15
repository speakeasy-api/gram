# Signup offers

Staff manage signup offers in **Admin → Signup offers**, independently of onboarding
playbooks. Create an offer with a name, an exact Stripe promotion code ID (`promo_…`),
and the **Tailscale Partner Gateway** flow (`tailscale_partner_gateway`). New offers
are disabled; enable them explicitly when ready. Configure discount terms in Stripe,
not in Speakeasy. Promotion code IDs are unique across offers.

Signup resolves the human-facing code through Stripe and then requires an exact,
enabled offer match for the supported flow. Eligibility is checked again in the
signup transaction, holding the offer row against concurrent edits until attribution
commits. Unmapped, disabled, and unsupported offers cannot qualify a new signup.

Partner signup does not start a trial or assign a playbook. Checkout automatically
applies the accepted promotion and requires a payment method. Private networking
is enabled only after verified PAYG activation. Ordinary signup is unchanged.

Accepted `organization_promotion_applications` remain the source of truth for retry,
checkout, redemption and audit. Editing or disabling an offer affects new signups
only. It neither rewrites accepted promotion IDs nor permits reuse after cancellation.
There is no partner-playbook environment configuration.

## Manual promotion entry in Checkout

An ordinary new organization can enter its partner promotion directly in Stripe
Checkout; there is no signup form code input. The server retrieves the completed
Checkout and its subscription, requires the exact applied promotion ID to match
an enabled gateway signup offer, and verifies gateway-only coupon scope. The
offer row is locked during first acceptance. Disabled, unmapped, absent, or
unsupported promotions remain ordinary discounts: they grant no partner rights
and do not change the existing ordinary Checkout trial behavior. A discount
shown by Stripe is not confirmation of private-network access.

Acceptance is committed before ending a qualifying remaining Stripe trial. The
pending promotion application and persisted Checkout session bind retries to the
same subscription. Offer edits or disabling after acceptance do not revoke that
acceptance. The server ends only that accepted partner trial, using an idempotent
`trial_end=now`, `proration_behavior=none` update. It then retrieves current Stripe
state under the activation locks. A collected payment method and an active
subscription are required before redemption, PAYG activation, audit, and private
network enablement commit together. `active` denotes a paid subscription, not a
guarantee that every metered invoice has already been collected.

A lost Stripe response retries from the immutable acceptance and live subscription
state, without issuing another trial-ending operation once active. A pending
completed Checkout cannot be replaced. Cancellation before activation leaves its
acceptance/session bound and unredeemed; reconciliation requires support rather
than silently starting a second subscription. Existing subscribers cannot obtain
first manual attribution. Explicit staff networking disables remain authoritative.

### Partner conversion accounting

Stripe resets the billing anchor to the exact trial-ending instant. It rejects
combining immediate trial termination with an unchanged anchor; subscription
update does not support `billing_cycle_anchor_config`. Consequently, only verified
redeemed partner subscriptions may use non-midnight monthly invoice periods.
Exact Stripe timestamps are retained and checked, including monthly day clamping
for short months. Later changes to the original billing anchor fail closed.

Stripe-native meters retain exact Stripe periods. Daily inference spend uses
completed UTC days: the initial partial conversion day is waived; each subsequent
boundary day belongs wholly to the following invoice. This assigns each eligible
full day once without pretending daily aggregates have intraday precision. The
same day selection applies to recovery, readiness, freezing, and settlement.
The original partner anchor is retained after cancellation for late settlement.
Staff subscription reassignment cannot overwrite a redeemed partner's anchor.

This is a partner-only accounting policy, not a change to ordinary trials,
ordinary midnight invoicing, or an entitlement for unrelated discounts.

### Manual-route MCP assessment

Customer Platform MCP needs no new tool: acceptance and payment-method collection
remain Stripe-hosted browser operations; agent-supplied promotion IDs must never
grant access. Existing network tools continue to enforce entitlements. Staff
Admin MCP needs no new mutation: its existing service-backed billing operations
inherit the retained-anchor safeguard. Shared offer management restrictions above
remain unchanged. The evidence of success is committed redemption plus verified
PAYG activation, not discount discovery or an agent assertion.

## Surface and seed assessment

- **Staff Admin MCP:** no new tool in this change. This is shared, pre-signup
  configuration, not an existing organization's configuration. The current
  tenant-targeted proposal/approval/write receipts do not cover this shared target;
  exposing a mutation without those boundaries would be inappropriate. Read-only
  offer discovery alone would not provide the approved management outcome.
- **Customer Platform MCP:** no tool change. Shared staff-only offers must not become
  tenant-visible configuration or an alternative way to grant networking access.
  Pre-signup code attribution and payment-method collection remain browser-owned:
  they do not provide an authenticated agent operation for assigning an offer or
  activating networking. Existing onboarding and network-access tools retain their
  current authorization and entitlement checks.
- **Subscription discount read:** assessed separately from shared offer management.
  This is an organization-scoped billing read for an authorized billing user, not
  a shared-configuration mutation. Existing Platform MCP tools do not expose a
  subscription/discount read. This change intentionally keeps verified discount
  display in the dashboard's payment workflow; it does not add a standalone agent
  billing workflow for either external users or managed assistants. The shared
  Admin MCP write restrictions are not a reason to prohibit such a read; adding
  one would require its own billing authorization and audience contract.
- **Demo seed:** no global signup offers are seeded. The customer demo is an existing,
  read-only organization and has no staff offer page. Seeding shared Stripe promotion
  configuration in its daily tenant reseed would affect other organizations' signup
  eligibility. Tests use fictional, isolated offer fixtures instead.

## Rollout

Ship the schema and Atlas-generated migration separately, before feature code.
Configure and enable real Stripe mappings through staff admin after deployment.
Do not put real promotion IDs, customer identifiers, or financial terms in fixtures
or committed configuration. Existing accepted applications require no backfill.
