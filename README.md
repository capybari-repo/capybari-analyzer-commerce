# capybari-analyzer-commerce (experimental)

**Capybari Source Intelligence: Trust & Commerce Readiness: can a visitor safely buy from, or sign up to, this site?**

Using only the pages the Website Snapshot already fetched (front page and up to 5 linked pages), it records:

- **payment providers** seen in scripts and links: Stripe, Paddle, Lemon Squeezy, PayPal, Gumroad, Shopify, WooCommerce, Chargebee, FastSpring, Polar, Braintree, Square, Razorpay, Mollie, Adyen, iyzico, Ko-fi, Buy Me a Coffee, App Store, Google Play, Steam, itch.io
- **entry points**: login, sign-up, checkout/buy links, pricing and whether prices are shown
- **trust paths**: privacy policy, terms, refund/cancellation policy, contact or support

It flags:

| Finding | When | For buyers |
|---|---|---|
| `insecure-credentials` | a password field on an `http://` page, or a form posting to `http://` | blocks purchase |
| `missing-legal` | takes payments with no terms or privacy link (or collects accounts with no privacy link) | blocks purchase (payments) / support cost (accounts) |
| `missing-refund` | takes payments with no refund, returns or cancellation link | support cost |
| `missing-contact` | no contact/support link, email or phone | support cost |
| `coming-soon` | the front page is an announcement or waitlist, with no login, checkout or payment | blocks purchase |
| `pricing-stub` | the pricing page shows no price, free plan or "contact sales" | support cost |
| `purchase-path-unverified` | prices or accounts but no payment provider or checkout on the web: "Purchase path is app stores only; web checkout not verified" (low), or "Prices shown, but no way to pay was found" (medium) | support cost |

It **never** opens login, checkout or account pages, submits a form, or contacts a payment provider. Its evidence (`commerce`) feeds the **Trust** and **Finish** questions of the report verdict.

| | |
|---|---|
| Requires | `web-snapshot` |
| Provides | `commerce` evidence |
| Scores | none (feeds the verdict) |
| Network | none |
| Rules | [`rules/signals.yaml`](rules/signals.yaml) |

```bash
go run ./cmd/capybari-commerce https://example.com
```

Methodology: [docs/methodology.md](docs/methodology.md). License: Apache-2.0.
