package bot

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-rod/rod"
)

// Native mappings use a verified issuer link. Without one, reuse stays within
// the listing; a display name alone cannot identify an employer across jobs.
func nativeEmployerScope(ctx context.Context, page *rod.Page, platform string, job linkedInJob) string {
	fallback := platform + ":listing:" + job.ID
	result, err := page.Context(ctx).Eval(`(company)=>{
  const norm=s=>String(s||'').replace(/\s+/g,' ').trim().toLowerCase();
  const links=[...document.querySelectorAll('.job-details-jobs-unified-top-card__company-name a,[data-automation="job-detail-company-name"] a,a[data-automation="job-detail-company-name"],[data-testid="job-detail-company-name"] a')].filter(el=>{const r=el.getBoundingClientRect();return r.width>0&&r.height>0&&norm(el.textContent)===norm(company);}).map(el=>el.href);
  const unique=[...new Set(links)];return unique.length===1?unique[0]:'';
 }`, job.Company)
	if err != nil || result == nil {
		return fallback
	}
	parsed, err := url.Parse(result.Value.String())
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return fallback
	}
	host := strings.ToLower(parsed.Hostname())
	if platform == "linkedin" && (host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com")) && strings.HasPrefix(parsed.Path, "/company/") {
		return platform + ":issuer:" + strings.TrimRight(parsed.Path, "/")
	}
	if platform == "seek" && trustedSeekURL(parsed.String()) && (strings.HasPrefix(parsed.Path, "/companies/") || strings.HasPrefix(parsed.Path, "/advertiser/")) {
		return platform + ":issuer:" + strings.TrimRight(parsed.Path, "/")
	}
	return fallback
}

func verifyAdaptiveSeekIdentity(ctx context.Context, page *rod.Page, lazy *lazyDocGen, source bool) error {
	if lazy == nil || page == nil || !trustedSeekURL(lazy.job.URL) {
		return fmt.Errorf("%w: missing intended SEEK listing", errLinkedInIdentity)
	}
	expectedID := seekJobIDFromURL(lazy.job.URL)
	currentURL := currentPageURL(page.Context(ctx))
	if expectedID == "" || lazy.job.ID != expectedID || !trustedSeekURL(currentURL) || seekJobIDFromURL(currentURL) != expectedID {
		return fmt.Errorf("%w: SEEK application left the intended listing", errLinkedInIdentity)
	}
	if !source {
		return nil
	}
	company, err := page.Context(ctx).Eval(`()=>{
  const texts=[...document.querySelectorAll('[data-automation="job-detail-company-name"],[data-testid="job-detail-company-name"],[data-automation="advertiser-name"]')].filter(el=>{const r=el.getBoundingClientRect();return r.width>0&&r.height>0;}).map(el=>el.textContent.trim());
  return [...new Set(texts)].length===1?texts[0]:'';
 }`)
	if err != nil || company == nil || strings.TrimSpace(lazy.job.Company) == "" || !strings.EqualFold(normalizeLabel(company.Value.String()), normalizeLabel(lazy.job.Company)) {
		return fmt.Errorf("%w: SEEK employer could not be verified", errLinkedInIdentity)
	}
	return nil
}

func seekExternalHandoff(ctx context.Context, page *rod.Page) (string, error) {
	destination, err := page.Context(ctx).Eval(`()=>{
  const links=[...document.querySelectorAll('a[data-automation="job-detail-apply"],a[data-automation="job-detail-apply-link"]')].filter(el=>{const r=el.getBoundingClientRect();return r.width>0&&r.height>0;});
  const unique=[...new Set(links.map(el=>el.href))];return unique.length===1?unique[0]:'';
 }`)
	if err != nil {
		return "", err
	}
	if destination == nil {
		return "", fmt.Errorf("SEEK handoff could not be read")
	}
	return destination.Value.String(), nil
}
