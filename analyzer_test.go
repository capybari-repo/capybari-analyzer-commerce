package commerce_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	commerce "github.com/capybari-repo/capybari-analyzer-commerce"
	"github.com/capybari-repo/capybari-core/analyzer"
	"github.com/capybari-repo/capybari-core/analyzertest"
	"github.com/capybari-repo/capybari-core/facts"
	"github.com/capybari-repo/capybari-core/finding"
	"github.com/capybari-repo/capybari-core/report"
	"github.com/capybari-repo/capybari-schemas"
	"gopkg.in/yaml.v3"
)

func TestCapabilityMetadata(t *testing.T) {
	b, err := os.ReadFile("capability.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := analyzer.ParseCapability(b); err != nil {
		t.Fatal(err)
	}
	var doc any
	yaml.Unmarshal(b, &doc)
	if err := schemas.ValidateValue("capability.schema.json", doc); err != nil {
		t.Fatal(err)
	}
}

// site serves a map of path -> HTML.
func site(pages map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(body))
	}))
}

func run(t *testing.T, pages map[string]string) (*report.Report, facts.Commerce, map[string]finding.Finding) {
	t.Helper()
	srv := site(pages)
	t.Cleanup(srv.Close)
	r := analyzertest.Run(t, commerce.New(), analyzertest.Website(srv.URL), analyzertest.Options{Online: true})
	var c facts.Commerce
	if !r.Fact(facts.KeyCommerce, &c) {
		t.Fatal("no commerce fact")
	}
	got := map[string]finding.Finding{}
	for _, f := range r.Findings {
		got[f.Category] = f
	}
	return r, c, got
}

const footer = `<footer><a href="/privacy">Privacy</a> <a href="/terms">Terms</a> <a href="/refunds">Refund policy</a> <a href="mailto:hello@tallyroom.co.uk">Contact</a></footer>`

func TestCommerceReadySite(t *testing.T) {
	_, c, got := run(t, map[string]string{
		"/": `<html><head><script src="https://js.stripe.com/v3/"></script></head><body><nav><a href="/pricing">Pricing</a> <a href="/login">Log in</a> <a href="/signup">Start free trial</a></nav>
<p>Rotas for cafés.</p>` + footer + `</body></html>`,
		"/pricing": `<html><body><h1>Pricing</h1><p>£19 per month per café.</p>` + footer + `</body></html>`,
	})
	if len(got) != 0 {
		t.Fatalf("a commerce-ready site has no findings: %+v", got)
	}
	if len(c.PaymentProviders) != 1 || c.PaymentProviders[0] != "Stripe" || !c.Login || !c.Signup || !c.Sells || !c.PricesShown ||
		c.Privacy == "" || c.Terms == "" || c.Refund == "" || c.Contact == "" || c.ComingSoon {
		t.Fatalf("commerce fact: %+v", c)
	}
}

func TestSellsWithoutLegalOrContact(t *testing.T) {
	_, c, got := run(t, map[string]string{
		"/":        `<html><body><h1>Course</h1><p>Learn everything.</p><a href="https://buy.stripe.com/abc">Buy now</a> <a href="/pricing">Pricing</a></body></html>`,
		"/pricing": `<html><body><h1>Pricing</h1><p>Plans coming later.</p></body></html>`,
	})
	if f := got["missing-legal"]; f.Impact == nil || f.Impact.Buyer != finding.BuyerBlocks {
		t.Fatalf("taking money without terms/privacy must block: %+v", got)
	}
	for _, cat := range []string{"missing-refund", "missing-contact", "pricing-stub"} {
		if _, ok := got[cat]; !ok {
			t.Fatalf("%s expected: %+v", cat, got)
		}
	}
	if !c.Checkout || c.PricesShown {
		t.Fatalf("fact: %+v", c)
	}
}

func TestComingSoonShell(t *testing.T) {
	_, c, got := run(t, map[string]string{
		"/": `<html><body><h1>Nimbus</h1><p>Something great is coming soon. Join the waitlist to get early access.</p><form><input type="email"><button>Notify me</button></form>` + footer + `</body></html>`,
	})
	if f, ok := got["coming-soon"]; !ok || f.Impact.Buyer != finding.BuyerBlocks || !c.ComingSoon {
		t.Fatalf("coming-soon shell: %+v %+v", got, c)
	}
}

func TestPasswordOverHTTP(t *testing.T) {
	// httptest serves plain http, which is exactly the insecure case.
	_, _, got := run(t, map[string]string{
		"/": `<html><body><form method="post"><label for="p">Password</label><input id="p" type="password"></form>` + footer + `</body></html>`,
	})
	if f, ok := got["insecure-credentials"]; !ok || f.Severity != finding.High || f.Impact.Buyer != finding.BuyerBlocks {
		t.Fatalf("password over http: %+v", got)
	}
}

func TestNewsAmountsAreNotPrices(t *testing.T) {
	_, c, got := run(t, map[string]string{
		"/": `<html><body><h1>News</h1><p>The city approved a $40 million budget; fuel rose to €1.90.</p><a href="mailto:desk@news.co">Contact</a></body></html>`,
	})
	if c.Sells || len(got) != 0 {
		t.Fatalf("amounts in news copy are not a shop: %+v %+v", c, got)
	}
}

func TestPlaceholderContactIsNotContact(t *testing.T) {
	_, c, got := run(t, map[string]string{
		"/": `<html><body><p>Write to example@example.com or hello@yourcompany.com.</p></body></html>`,
	})
	if c.Contact != "" || got["missing-contact"].Severity != finding.Low {
		t.Fatalf("placeholder addresses are not contact details: %+v %+v", c, got)
	}
}

// A free product or an app sold through a store takes no money on the site,
// so it needs no refund policy; dead "#" links are not contact paths.
func TestFreeProductAndStore(t *testing.T) {
	_, c, got := run(t, map[string]string{
		"/": `<html><body><h1>Tally</h1><p>Free forever. See our <a href="/pricing">pricing</a>.</p>
<a href="https://play.google.com/store/apps/details?id=x">Get it on Google Play</a> <a href="#">Contact</a>
<a href="/privacy">Privacy</a> <a href="/terms">Terms</a></body></html>`,
	})
	if _, ok := got["missing-refund"]; ok {
		t.Fatalf("free product / store app needs no refund policy: %+v", got)
	}
	if f := got["purchase-path-unverified"]; f.Title != "Purchase path is app stores only; web checkout not verified" {
		t.Fatalf("store-only purchase path: %+v", got)
	}
	if len(c.Stores) != 1 || c.Stores[0] != "Google Play" || len(c.PaymentProviders) != 0 || !c.Sells {
		t.Fatalf("store: %+v", c)
	}
	if c.Contact != "" || got["missing-contact"].Title == "" {
		t.Fatalf(`a "#" link is not a contact path: %+v`, c)
	}
}
