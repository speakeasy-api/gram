# Embedded Checkout form: product breakdown during a metered trial

## Conclusion

**The documented native option is `features.amountSummary.expanded: 'always'`, but it controls expansion, not which line items or rates Stripe includes.** Neither the JavaScript nor React reference promises a hosted-style product breakdown or per-unit metered rates during a subscription trial. No separate native inclusion switch was found in the sources below. This is **not proof that the form cannot render those details**: eligibility, configuration, session composition, or an undocumented capability remain open questions for Stripe.

Scope: the new `ui_mode: 'form'`, `CheckoutFormProvider` / `CheckoutForm` integration—not full-page `EmbeddedCheckout`, and not a custom Elements integration. Research was read-only against official documentation; no Stripe sessions or configuration were changed.

## What the documentation actually establishes

### 1. Expansion is not inclusion

The [JavaScript `createForm` reference](https://docs.stripe.com/js/custom_checkout/create_form.md) and [React `CheckoutForm` reference](https://docs.stripe.com/js/react_stripe_js/checkout_form/checkout_form.md) give identical semantics for `features.amountSummary.expanded`:

> Controls whether the amount summary can be collapsed in the embedded form.
>
> By default (`'auto'`), the amount summary starts collapsed when it has expandable content, and the customer can expand or collapse it.
>
> Set to `'always'` to keep the amount summary expanded and prevent it from being collapsed.
>
> If there are no additional details to display, the summary shows only the total and doesn't expand, regardless of this setting.

Thus `'always'` is a supported native setting worth retaining; it does not force creation of additional detail. The references do not enumerate what counts as “additional details,” or define a metered-trial item visibility rule. The documented `features` controls also include `promotionCodeCollection`; it controls the promo input, not itemization.

### 2. Form layout is a different control

The [actual form quickstart](https://docs.stripe.com/checkout/form/quickstart.md) explicitly creates sessions with `ui_mode=form`, uses `CheckoutFormProvider` / `CheckoutForm` in React, and `initCheckoutFormSdk` / `createForm` in JavaScript. Its layout setting is:

- `layout: 'expanded'`: single-step checkout.
- `layout: 'compact'`: multi-step checkout, private preview.
- Unset: Stripe chooses the layout it determines has the best conversion.

This is independent of amount-summary expansion. For a deterministic native baseline, the documented combination to try is:

```tsx
<CheckoutForm
  options={{
    layout: "expanded",
    features: { amountSummary: { expanded: "always" } },
  }}
  onConfirm={onConfirm}
/>
```

It is **not** a documented fix for missing product rows. If both settings are already active, there is no further documented item-inclusion toggle identified here. The similarly named `/checkout/embedded/quickstart` describes the full embedded page and must not be used as evidence for form capabilities.

### 3. Subscription line-item data exists, but rendering is underspecified

The [form-specific subscription guide](https://docs.stripe.com/payments/checkout/build-subscriptions.md?payment-ui=checkout-form) says:

> The Checkout Session you created on the server automatically determines the line items, total amount, and available payment methods. The embedded form uses this information to display the appropriate interface.

That confirms the form receives a session-driven subscription context. It does **not** explicitly promise that every session line item appears as a visible product row, nor demonstrate metered unit-rate rendering during a trial. Its introductory examples use fixed recurring prices, so they cannot establish the behavior of a usage-only trial.

The [Product object reference](https://docs.stripe.com/api/products/object.md) says `unit_label` represents units of the product and, when set, is included in receipts, invoices, Checkout, and the customer portal. This is relevant catalog metadata to verify, but it is a broad Checkout statement—not a form-specific guarantee that setting it reveals hidden line items or rates. Do not change product metadata merely to force visibility without validating the effect.

### 4. Trial billing explains a zero current charge, not hidden item names

[Free trial periods](https://docs.stripe.com/billing/subscriptions/trials/free-trials.md#trials-with-usage-based-billing) explicitly support usage-based subscriptions: usage recorded during the trial is not billed; billing resumes for recorded usage after the trial ends. The [usage-based implementation guide](https://docs.stripe.com/billing/subscriptions/usage-based/implementation-guide.md) describes pay-as-you-go usage billed in arrears at the end of the billing interval.

These facts are consistent with a zero amount today and a usage-based future total. They do not explain or require suppression of product names or per-unit rates. A rate can be known even when future usage and its total are unknown.

Caution: the current `/billing/subscriptions/trials` page documents the newer **Trial Offers** feature, not just `trial_end` / `trial_period_days`; it links legacy free trial periods separately. Do not conflate the two or recommend a billing-model migration to solve a presentation issue.

### 5. Studio is a useful inspection path, not a verified hidden switch

[Checkout studio](https://docs.stripe.com/payments/checkout-studio.md) advertises a visual editor for embedded form integrations, branding/layout, and session behavior. [How Studio works](https://docs.stripe.com/payments/checkout-studio/how-it-works.md) says settings map to Checkout Session parameters, configuration remains available through the API, and the setup path is **Checkouts → Create checkout → Web → embedded form → capabilities/design → Implement**.

The public guides do not name a “show line items,” “show product details,” or “show metered rates” control. Inspecting the actual editor and its generated implementation for such a setting is a reasonable next step, but the docs alone do not establish that it exists. The [form appearance guide](https://docs.stripe.com/payments/checkout/customization/appearance.md?payment-ui=checkout-form) documents visual styling, not a data-inclusion API.

### 6. Changelog confirms distinct products, not order-summary parity

The [March 25, 2026 UI-mode changelog](https://docs.stripe.com/changelog/dahlia/2026-03-25/updates-available-checkout-session-ui-modes.md) distinguishes `hosted_page`, `embedded_page`, `elements`, and the newly added `form`. It describes default Adaptive Pricing and improved payment-method handling for `form`; it does not promise hosted order-summary parity. The [embedded initialization rename](https://docs.stripe.com/changelog/dahlia/2026-03-25/rename-init-embedded-checkout-to-create-embedded-checkout-page.md) concerns `createEmbeddedCheckoutPage`, not `CheckoutForm`.

Searching the official [changelog index](https://docs.stripe.com/changelog.md) and its Dahlia/Clover release summaries did not identify a release note promising metered-trial form itemization or a switch enabling it. This is a bounded negative search result, not a statement about all Stripe releases or account-specific previews.

## Observed behavior versus supported conclusions

The reported current UI shows “Total Usage-based,” promotion entry, zero due today, and the monthly billing start date, but no product rows, despite `amountSummary.expanded: 'always'`. This observation was supplied with the assignment and **was not independently reproduced by this research**.

It establishes that expansion alone did not produce the requested breakdown in that session. It does not establish whether the cause is trial status, metered-only items, form design, configuration, or a rollout/bug.

## Recommended next steps

1. Verify `layout: 'expanded'` alongside the existing amount-summary setting; verify session price/product associations and the actual returned metered price data. No speculative option names.
2. With authorization, compare fresh test sessions: fixed recurring; fixed recurring with trial; metered without trial; metered with trial. Keep form settings constant. This isolates trial-specific versus usage-specific behavior without introducing custom UI.
3. Inspect Studio's embedded-form capabilities/design and generated implementation. Treat any discovered option as a candidate to validate, not an assumed solution.
4. Ask Stripe: **“For `ui_mode=form` and `CheckoutForm`, can the native amount summary display each subscription product name and metered unit rate during a free trial? `features.amountSummary.expanded='always'` shows totals only. Is itemization supported, gated, or controlled by a documented session/form/Studio setting? Does trial or metered-only composition affect eligibility?”** Include a sanitized reproduction privately.
5. Until validated, describe the result as **“no documented native itemization control found; current session does not show the breakdown,”** not “impossible.” Do not silently substitute full embedded-page Checkout or custom Elements: those change the requested integration.
