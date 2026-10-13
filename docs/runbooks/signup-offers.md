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

## Surface and seed assessment

- **Staff Admin MCP:** no new tool in this change. This is shared, pre-signup
  configuration, not an existing organization's configuration. The current
  tenant-targeted proposal/approval/write receipts do not cover this shared target;
  exposing a mutation without those boundaries would be inappropriate. Read-only
  offer discovery alone would not provide the approved management outcome.
- **Customer Platform MCP:** no tool change. Shared staff-only offers must not become
  tenant-visible configuration or an alternative way to grant networking access.
- **Demo seed:** no global signup offers are seeded. The customer demo is an existing,
  read-only organization and has no staff offer page. Seeding shared Stripe promotion
  configuration in its daily tenant reseed would affect other organizations' signup
  eligibility. Tests use fictional, isolated offer fixtures instead.

## Rollout

Ship the schema and Atlas-generated migration separately, before feature code.
Configure and enable real Stripe mappings through staff admin after deployment.
Do not put real promotion IDs, customer identifiers, or financial terms in fixtures
or committed configuration. Existing accepted applications require no backfill.
