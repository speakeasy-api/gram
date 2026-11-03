# Embedded Checkout layout and styling

## Scope and result

Official Stripe documentation for `@stripe/react-stripe-js` **`EmbeddedCheckout`**, not `PaymentElement`. No application code or live Stripe settings changed. Sources were fetched from official `docs.stripe.com` Markdown endpoints.

**Improve the embedded form through Checkout branding settings; improve its surrounding layout through the host container.** Changing a container's height-to-width ratio does not style the iframe's contents. No numeric width, height, or aspect-ratio requirement was found in the references below.

## Supported styling controls

[Full embedded page appearance][appearance] explicitly supports Dashboard [Checkout Branding Settings](https://dashboard.stripe.com/settings/branding/checkout), or per-Session `branding_settings`:

| Control         | Documented API field / values                                                                                               |
| --------------- | --------------------------------------------------------------------------------------------------------------------------- |
| Background      | `background_color`: hex color beginning with `#`                                                                            |
| Button          | `button_color`: hex color beginning with `#`                                                                                |
| Corners / shape | `border_style`: `rounded`, `rectangular`, or `pill`                                                                         |
| Font            | `font_family`: supported enum, including `inter`, `roboto`, and `default`; not an arbitrary CSS font stack or uploaded font |
| Brand name      | `display_name`: overrides the name at the top; does not replace the business name in terms, receipts, or other locations    |

The exact fields and enums come from the [Checkout Session creation API][branding-api]. `border_style` is a **corner-shape** control, not a documented arbitrary border color/width or pixel-radius setting. The embedded appearance guide does not expose arbitrary input spacing, labels, typography sizes, shadows, or CSS selectors.

Important source excerpts:

- “If you omit a field in `branding_settings`, Checkout applies the default value from the Dashboard.” [Appearance][appearance]
- “Invoices from a Checkout Session still use the branding settings set in the Dashboard.” [Appearance][appearance]
- “CSS from your host page—including stylesheets, CSS variables, and component framework styles—doesn’t cross the iframe boundary and has no effect on the embedded form’s appearance.” [Appearance][appearance]
- Stripe directs developers to Dashboard Branding Settings or the Session `branding_settings` parameter instead. [Appearance][appearance]

**Appearance API distinction:** Stripe's [Checkout comparison][comparison] assigns brand settings to the full-page product (hosted or embedded), while separately assigning the Appearance API to its newer “Embedded form” product and Elements. [Payment Element documentation][payment-element] explicitly uses the Appearance API. Do not treat all things called “embedded” as the same integration or pass Elements appearance rules to `EmbeddedCheckoutProvider`: that is not a documented styling interface for this component. The [Embedded Checkout JS reference][js] does not list an `appearance` option.

Current docs use names such as `embedded_page` and `createEmbeddedCheckoutPage`; existing SDK/API versions may use older names. The [accept-a-payment guide][accept] still explicitly demonstrates `EmbeddedCheckoutProvider` and `EmbeddedCheckout`, establishing relevance to this integration. Confirm the repository's pinned Stripe API/SDK supports `branding_settings` before implementing per-Session overrides; this research did not inspect or change application dependencies.

## Official layout guidance and limits of the evidence

| Topic                       | What the primary sources actually say                                                                                                                                                                                                                                                                                                                         |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Outer spacing               | “By default, Checkout renders with no external padding or margin. We recommend using a container element such as a div to apply your desired margin (for example, 16px on all sides).” [Accept a payment][accept] **16px is an example, not a required minimum.**                                                                                             |
| Background matching         | “Customize Checkout to match the design of your site by setting the background color, button color, border radius, and fonts” in branding settings. [Accept a payment][accept] Matching the container background to the selected Checkout background is a reasonable design application of this advice, **not an explicit exact-color-equality requirement**. |
| Width / min / max width     | The React example mounts inside `<div id="checkout">` without a numeric width. No numeric minimum, maximum, preferred width, or mandated full-width rule was found in the reviewed Embedded Checkout guide, quickstart, appearance guide, or JS reference. [Accept a payment][accept], [Quickstart][quickstart], [JS reference][js]                           |
| Height / automatic resizing | Those references provide no required height, height-to-width formula, or explicit auto-resize guarantee. The React example supplies no height prop or sizing CSS. Letting Stripe manage the mounted content instead of imposing a fixed ratio is an **implementation recommendation inferred from the integration pattern**, not a quoted resize contract.    |
| Aspect ratios / `cqw`       | No recommendation for `75cqw`, `80cqw`, `100cqw`, or any aspect ratio was found in those references. Such minimum heights are local layout/loading-space choices, not Stripe sizing recommendations.                                                                                                                                                          |
| Nested iframes              | “Avoid placing Checkout within another iframe because some payment methods require redirecting to another page for payment confirmation.” [Accept a payment][accept] This does **not** prohibit Checkout's own iframe or a normal dashboard div/column.                                                                                                       |

## Actionable recommendation for the existing integration

1. **Keep `EmbeddedCheckout`.** Start with supported branding: background, brand button color, matching supported font, and corner preset. No integration switch is needed for these changes.
2. Prefer per-Session branding when styling should be scoped to this flow; otherwise use the Dashboard defaults deliberately, recognizing their wider effect. This is a scoping recommendation, not a Stripe mandate.
3. Style the host wrapper for available width, surrounding background, and spacing. Stripe explicitly recommends a wrapper for external margin. A full-width dashboard column is compatible with the documented mounting pattern, but is not a documented width requirement.
4. Treat any large `min-height` as a local loading/layout choice. Do not use a width-derived minimum height as a substitute for form styling or describe it as officially recommended. Test actual content expansion and narrow screens before deciding whether a reservation is useful.
5. Only consider another integration if requirements exceed the documented branding controls (for example, arbitrary field-level styling). The Appearance API on another product is not a drop-in option for this component.

## Primary sources

[accept]: https://docs.stripe.com/payments/accept-a-payment?payment-ui=checkout&ui=embedded-page
[appearance]: https://docs.stripe.com/payments/checkout/customization/appearance?payment-ui=embedded-page
[branding-api]: https://docs.stripe.com/api/checkout/sessions/create?query=branding_settings
[comparison]: https://docs.stripe.com/payments/checkout
[payment-element]: https://docs.stripe.com/payments/payment-element
[js]: https://docs.stripe.com/js/embedded_checkout
[quickstart]: https://docs.stripe.com/checkout/embedded/quickstart

- [Accept a payment — full embedded page][accept]: React component example, margin recommendation, iframe warning.
- [Customize appearance — full embedded page][appearance]: branding, iframe CSS boundary, Dashboard defaults, invoice caveat.
- [Create a Checkout Session — branding settings][branding-api]: exact fields and enum values.
- [Checkout overview][comparison]: distinguishes full page, newer embedded form, and Elements customization surfaces.
- [Payment Element][payment-element]: Appearance API belongs to that integration.
- [Embedded Checkout JS reference][js]: documented mounting and initialization interface.
- [Embedded Checkout quickstart][quickstart]: placeholder div and iframe guidance.
