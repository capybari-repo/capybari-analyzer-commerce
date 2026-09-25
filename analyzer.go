// Package commerce implements Trust & Commerce Readiness: what a visitor
// can see before trusting a website with money or an account. It reads only
// the pages the Website Snapshot already fetched; it never follows login or
// checkout links and never contacts a payment provider.
package commerce

import (
	"context"
	_ "embed"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/capybari-repo/capybari-core/analyzer"
	"github.com/capybari-repo/capybari-core/facts"
	"github.com/capybari-repo/capybari-core/finding"
	"github.com/capybari-repo/capybari-core/webtext"
)

//go:embed capability.yaml
var capabilityYAML []byte

//go:embed rules/signals.yaml
var signalsYAML []byte

var capability = analyzer.MustParseCapability(capabilityYAML)

type signalSet struct {
	Providers []struct {
		Name    string `yaml:"name"`
		Store   bool   `yaml:"store"`
		Pattern string `yaml:"pattern"`
	} `yaml:"providers"`
	Links      map[string]string `yaml:"links"`
	ComingSoon string            `yaml:"coming_soon"`
	Money      string            `yaml:"money"`
	Price      string            `yaml:"price"`
}

type provider struct {
	name  string
	store bool
	re    *regexp.Regexp
}

var (
	providers    []provider
	linkRules    = map[string]*regexp.Regexp{}
	reComingSoon *regexp.Regexp
	rePrice      *regexp.Regexp
	reMoney      *regexp.Regexp
)

func init() {
	var s signalSet
	if err := yaml.Unmarshal(signalsYAML, &s); err != nil {
		panic(err)
	}
	for _, p := range s.Providers {
		providers = append(providers, provider{p.Name, p.Store, regexp.MustCompile(p.Pattern)})
	}
	for k, v := range s.Links {
		linkRules[k] = regexp.MustCompile(v)
	}
	reComingSoon = regexp.MustCompile(s.ComingSoon)
	rePrice = regexp.MustCompile(s.Price)
	reMoney = regexp.MustCompile(s.Money)
}

var (
	reAnchor   = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	reHref     = regexp.MustCompile(`(?i)\bhref\s*=\s*["']([^"']*)["']`)
	rePassword = regexp.MustCompile(`(?i)<input\b[^>]*type\s*=\s*["']?password`)
	reHTTPForm = regexp.MustCompile(`(?i)<form\b[^>]*action\s*=\s*["']http://[^"']+`)
	reMail     = regexp.MustCompile(`(?i)(?:mailto:|tel:)[^"'\s>]+|[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}`)
	reExample  = regexp.MustCompile(`(?i)example\.|@domain\.|yourcompany|your-?email`)
)

// Analyzer implements the capability.
type Analyzer struct{}

// New returns the capability.
func New() *Analyzer { return &Analyzer{} }

// Capability implements analyzer.Analyzer.
func (*Analyzer) Capability() analyzer.Capability { return capability }

type page struct {
	url, html, text string
}

func pagesOf(ws *facts.WebSnapshot) []page {
	ps := []page{{ws.FinalURL, ws.Body, webtext.Visible(ws.Body)}}
	for _, p := range ws.Pages {
		ps = append(ps, page{p.URL, p.HTML, p.Text})
	}
	return ps
}

// Applies declines when no page was fetched.
func (*Analyzer) Applies(in *analyzer.Input) (bool, string) {
	var ws facts.WebSnapshot
	if ok, _ := in.Evidence.Get(facts.KeyWebSnapshot, &ws); !ok || ws.Body == "" {
		return false, "no page content was fetched"
	}
	return true, ""
}

type link struct{ href, text, page string }

// dead reports whether a link goes nowhere (a placeholder "#" or script).
func dead(href string) bool {
	h := strings.TrimSpace(strings.ToLower(href))
	return h == "" || h == "#" || strings.HasPrefix(h, "javascript:")
}

func links(ps []page) []link {
	var out []link
	for _, p := range ps {
		for _, m := range reAnchor.FindAllStringSubmatch(p.html, -1) {
			href := ""
			if h := reHref.FindStringSubmatch(m[1]); h != nil {
				href = h[1]
			}
			out = append(out, link{href: href, text: strings.TrimSpace(webtext.Visible(m[2])), page: p.url})
		}
	}
	return out
}

// resolve makes href absolute against base for evidence.
func resolve(base, href string) string {
	b, err := url.Parse(base)
	if err != nil {
		return href
	}
	h, err := b.Parse(href)
	if err != nil {
		return href
	}
	return h.String()
}

// Analyze implements analyzer.Analyzer.
func (*Analyzer) Analyze(_ context.Context, in *analyzer.Input) (*analyzer.Result, error) {
	var ws facts.WebSnapshot
	if _, err := in.Evidence.Get(facts.KeyWebSnapshot, &ws); err != nil {
		return nil, err
	}
	ps := pagesOf(&ws)
	c := &facts.Commerce{}
	all := ""
	for _, p := range ps {
		all += p.html + "\n"
	}
	for _, r := range ws.Resources {
		all += r.URL + "\n"
	}
	for _, p := range providers {
		switch {
		case !p.re.MatchString(all):
		case p.store:
			c.Stores = append(c.Stores, p.name)
		default:
			c.PaymentProviders = append(c.PaymentProviders, p.name)
		}
	}

	// Entry points and trust paths from link text and targets.
	found := map[string]string{}
	for _, l := range links(ps) {
		if dead(l.href) {
			continue
		}
		label := l.text + " " + l.href
		for kind, re := range linkRules {
			if _, ok := found[kind]; !ok && re.MatchString(label) {
				found[kind] = resolve(l.page, l.href)
			}
		}
	}
	c.Login, c.Signup = found["login"] != "", found["signup"] != ""
	c.Checkout = found["checkout"] != ""
	c.Privacy, c.Terms, c.Refund = found["privacy"], found["terms"], found["refund"]
	c.Contact = found["contact"]
	if c.Contact == "" {
		for _, m := range reMail.FindAllString(all, -1) {
			if !reExample.MatchString(m) {
				c.Contact = m
				break
			}
		}
	}

	// Pricing: prices on the front page, or on a fetched pricing page.
	if rePrice.MatchString(ps[0].text) && linkRules["pricing"].MatchString(ps[0].text) {
		c.Pricing, c.PricesShown = "front page", true
	}
	var pricingPage *page
	if p := found["pricing"]; p != "" && !c.PricesShown {
		c.Pricing = p
		for i := range ps {
			if strings.TrimSuffix(ps[i].url, "/") == strings.TrimSuffix(p, "/") {
				pricingPage = &ps[i]
				c.PricesShown = rePrice.MatchString(ps[i].text)
			}
		}
	}
	// Money changes hands on the site itself: a payment provider, a checkout
	// link, or an amount in a currency ("free" and "contact sales" do not
	// count). Stores handle payment and refunds themselves.
	// Amounts count only in a pricing context (news and blog copy mention
	// prices too), and not when a store sells the product.
	pricingWords := linkRules["pricing"].MatchString(ps[0].text)
	money := reMoney.MatchString(ps[0].text) && pricingWords || pricingPage != nil && reMoney.MatchString(pricingPage.text)
	takesMoney := len(c.PaymentProviders) > 0 || c.Checkout || money && len(c.Stores) == 0
	c.Sells = takesMoney || c.PricesShown || len(c.Stores) > 0 || c.Login || c.Signup

	front := ps[0]
	words := webtext.Words(front.text)
	c.ComingSoon = reComingSoon.MatchString(front.text) && words < 300 && !c.Login && !c.Checkout && len(c.PaymentProviders) == 0 && len(c.Stores) == 0

	var fs []finding.Finding
	add := func(f finding.Finding) {
		f.Dimension = finding.DimTrust
		if f.Rule == nil {
			f.Rule = &finding.Rule{ID: f.Category}
		}
		fs = append(fs, f)
	}

	// Credentials typed into an unencrypted page or sent to one.
	for _, p := range ps {
		insecurePage := strings.HasPrefix(p.url, "http://") && rePassword.MatchString(p.html)
		insecureForm := rePassword.MatchString(p.html) && reHTTPForm.MatchString(p.html)
		if insecurePage || insecureForm {
			add(finding.Finding{
				Category: "insecure-credentials", Severity: finding.High, Confidence: finding.ConfidenceHigh,
				Title:       "Password field on an unencrypted connection",
				Description: "A login or sign-up form on this page sends passwords without HTTPS, where anyone on the network path can read them.",
				Evidence:    []finding.Evidence{{Location: finding.Location{URL: p.url}, Detail: "password input on an http:// page or posting to an http:// address"}},
				Impact:      &finding.Impact{Business: "Accounts can be taken over.", Buyer: finding.BuyerBlocks},
				Remediation: &finding.Remediation{Summary: "Serve every page with a form over HTTPS and post only to https:// addresses."},
			})
			break
		}
	}

	if takesMoney && c.Privacy == "" && c.Terms == "" {
		add(finding.Finding{
			Category: "missing-legal", Severity: finding.Medium, Confidence: finding.ConfidenceMedium,
			Title:                 "Takes payments but links no terms or privacy policy",
			Description:           fmt.Sprintf("The site shows %s but none of the %d page(s) read link to terms of service or a privacy policy. Buyers have nothing that says what they are agreeing to or how their data is handled.", sellingWhat(c), len(ps)),
			Evidence:              []finding.Evidence{{Location: finding.Location{URL: ws.FinalURL}, Detail: "no link whose text or address mentions privacy, terms or legal"}},
			Impact:                &finding.Impact{Buyer: finding.BuyerBlocks},
			Remediation:           &finding.Remediation{Summary: "Publish terms of service and a privacy policy and link them from the footer of every page."},
			FalsePositiveGuidance: "Only the front page and up to 5 linked pages are read; links in menus built after a click are not seen.",
		})
	} else if c.Sells && c.Privacy == "" {
		add(finding.Finding{
			Category: "missing-legal", Severity: finding.Medium, Confidence: finding.ConfidenceMedium,
			Title:       "Collects accounts but links no privacy policy",
			Description: "The site offers sign-up or login but no page read links to a privacy policy.",
			Evidence:    []finding.Evidence{{Location: finding.Location{URL: ws.FinalURL}, Detail: "no link whose text or address mentions privacy"}},
			Impact:      &finding.Impact{Buyer: finding.BuyerSupportCost},
			Remediation: &finding.Remediation{Summary: "Publish a privacy policy and link it from the footer."},
		})
	}
	if takesMoney && c.Refund == "" {
		add(finding.Finding{
			Category: "missing-refund", Severity: finding.Low, Confidence: finding.ConfidenceLow,
			Title:       "No refund or cancellation policy linked",
			Description: "The site takes payments, but no page read links to a refund, returns or cancellation policy. It may be inside the terms.",
			Evidence:    []finding.Evidence{{Location: finding.Location{URL: ws.FinalURL}, Detail: "no link mentioning refund, returns or cancellation"}},
			Impact:      &finding.Impact{Buyer: finding.BuyerSupportCost},
			Remediation: &finding.Remediation{Summary: "State how to cancel and get a refund, and link it near pricing and checkout."},
		})
	}
	if c.Contact == "" {
		sev, buyer := finding.Low, finding.BuyerSupportCost
		if c.Sells {
			sev = finding.Medium
		}
		add(finding.Finding{
			Category: "missing-contact", Severity: sev, Confidence: finding.ConfidenceMedium,
			Title:       "No way to contact the people behind the site",
			Description: "No contact or support link, email address or phone number was found on the pages read.",
			Evidence:    []finding.Evidence{{Location: finding.Location{URL: ws.FinalURL}, Detail: fmt.Sprintf("%d page(s) read", len(ps))}},
			Impact:      &finding.Impact{Buyer: buyer},
			Remediation: &finding.Remediation{Summary: "Add a contact or support page, or at least an email address, in the footer."},
		})
	}
	if c.ComingSoon {
		m := reComingSoon.FindString(front.text)
		add(finding.Finding{
			Category: "coming-soon", Severity: finding.Medium, Confidence: finding.ConfidenceMedium,
			Title:       "The site is an announcement, not a product yet",
			Description: fmt.Sprintf("The front page says %q, has %d words, and offers no login, checkout or payment. There is nothing to use or buy yet.", m, words),
			Evidence:    []finding.Evidence{{Location: finding.Location{URL: ws.FinalURL}, Snippet: m}},
			Impact:      &finding.Impact{Buyer: finding.BuyerBlocks},
		})
	}
	if pricingPage != nil && !c.PricesShown {
		add(finding.Finding{
			Category: "pricing-stub", Severity: finding.Low, Confidence: finding.ConfidenceLow,
			Title:       "Pricing page shows no prices",
			Description: "The pricing page was read but shows no price, free plan or 'contact sales' option.",
			Evidence:    []finding.Evidence{{Location: finding.Location{URL: pricingPage.url}}},
			Impact:      &finding.Impact{Buyer: finding.BuyerSupportCost},
			Remediation: &finding.Remediation{Summary: "Show prices, or say how pricing works (free, per seat, contact sales)."},
		})
	}

	return &analyzer.Result{
		Findings: fs,
		Summary:  summary(c, len(ps)),
		Evidence: map[string]any{facts.KeyCommerce: c},
		Limitations: []string{
			"Only public pages are read: login, checkout and account pages are never opened, so whether checkout actually completes is not checked.",
		},
	}, nil
}

func sellingWhat(c *facts.Commerce) string {
	switch {
	case len(c.PaymentProviders) > 0:
		return "payments via " + strings.Join(c.PaymentProviders, ", ")
	case c.Checkout:
		return "a buy or checkout link"
	}
	return "prices"
}

func summary(c *facts.Commerce, pages int) string {
	var has, missing []string
	add := func(ok bool, name string) {
		if ok {
			has = append(has, name)
		} else {
			missing = append(missing, name)
		}
	}
	add(c.Privacy != "", "privacy policy")
	add(c.Terms != "", "terms")
	add(c.Refund != "", "refund policy")
	add(c.Contact != "", "contact")
	entry := []string{}
	for _, e := range []struct {
		ok   bool
		name string
	}{{c.Login, "login"}, {c.Signup, "sign-up"}, {c.Checkout, "checkout"}, {c.PricesShown, "prices"}} {
		if e.ok {
			entry = append(entry, e.name)
		}
	}
	pay := "no payment provider seen"
	if len(c.PaymentProviders) > 0 {
		ps := append([]string{}, c.PaymentProviders...)
		sort.Strings(ps)
		pay = "payments: " + strings.Join(ps, ", ")
	}
	if len(c.Stores) > 0 {
		pay += "; stores: " + strings.Join(c.Stores, ", ")
	}
	s := fmt.Sprintf("Read %d page(s): %s", pages, pay)
	if len(entry) > 0 {
		s += "; " + strings.Join(entry, ", ")
	}
	if len(has) > 0 {
		s += "; has " + strings.Join(has, ", ")
	}
	if len(missing) > 0 {
		s += "; no " + strings.Join(missing, ", ")
	}
	if c.ComingSoon {
		s += "; coming-soon page"
	}
	return s
}
