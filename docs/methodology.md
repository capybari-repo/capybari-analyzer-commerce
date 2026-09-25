# Methodology: Trust & Commerce Readiness (experimental)

## Principles

- **A visitor's view only.** Everything comes from pages the Website Snapshot already fetched with ordinary GET requests. Login, checkout and account pages are never opened, forms are never submitted, and payment providers are never contacted.
- **Presence, not quality.** A linked privacy policy is recorded as present; its content is not judged.
- **Buyer impact is explicit.** Every finding says whether it blocks a purchase, raises support cost, or is cosmetic.

## What is recorded (`commerce` evidence)

| Field | How |
|---|---|
| `payment_providers` | patterns in page markup and resource URLs ([rules/signals.yaml](../rules/signals.yaml)) |
| `login`, `signup`, `checkout` | link text or target on any fetched page (e.g. "Log in", `/signup`, "Buy now", `/cart`) |
| `pricing`, `prices_shown` | a pricing link; prices, a free plan or "contact sales" on the front page or the fetched pricing page |
| `privacy`, `terms`, `refund`, `contact` | link text or target; contact also accepts a real email address or phone number (placeholders such as example@example.com do not count) |
| `sells` | a payment provider, checkout link, visible prices, login or sign-up |
| `coming_soon` | the front page says "coming soon", "join the waitlist"… **and** has under 300 words **and** offers no login, checkout or payment |

Several languages are recognised for the common words (English, German, Turkish, French legal notice).

## Findings

| Rule | Trigger | Severity | Confidence | Buyer impact |
|---|---|---|---|---|
| `insecure-credentials` | password input on an `http://` page, or a form posting to `http://` | high | high | blocks purchase |
| `missing-legal` | takes money, no privacy **and** no terms link | medium | medium | blocks purchase |
| `missing-legal` | accounts only, no privacy link | medium | medium | support cost |
| `missing-refund` | takes money, no refund/returns/cancellation link | low | low | support cost |
| `missing-contact` | no contact path at all | medium if it sells, else low | medium | support cost |
| `coming-soon` | see above | medium | medium | blocks purchase |
| `pricing-stub` | fetched pricing page with no price, free plan or contact-sales option | low | low | support cost |

## Limitations

Only the front page and up to 5 linked pages are read, so a legal page linked only from a page that was not read can be missed. Menus built after a click are not seen. Whether checkout actually completes is not checked.
