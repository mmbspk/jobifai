package bot

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/go-rod/rod"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

var errAdaptiveDocumentReview = errors.New("document attachment needs your review")

// Resolve the current approved document through the same policy used by Legacy.
// An opaque field ref is never used as an ordinal among file inputs.
func (b *Bot) fillAdaptiveDocument(ctx context.Context, page *rod.Page, lazy *lazyDocGen, field SnapField) (result FieldFillResult) {
	defer func() {
		if result.Err != nil {
			var hold *uploadIdentityHold
			if lazy != nil && errors.As(result.Err, &hold) {
				lazy.holdReason, lazy.holdStage, lazy.holdFile = hold.Error(), hold.stage, hold.filename
			}
			// A chooser click or file delivery may already have happened. Retrying
			// the same opaque slot can dispatch another upload while the first is
			// still processing; every uncertain document result stops this step.
			result.Err = fmt.Errorf("%w: %w", errAdaptiveDocumentReview, result.Err)
		}
	}()
	if b == nil || lazy == nil || page == nil || field.BrowserField == nil {
		return FieldFillResult{Err: fmt.Errorf("adaptive: upload lacks current scanned control or approved document context")}
	}
	f := *field.BrowserField
	if f.Type != "file" {
		return FieldFillResult{Err: fmt.Errorf("adaptive: upload target is not a file control")}
	}
	currentFields, _, err := b.scanAdaptiveStep(ctx, page)
	if err != nil {
		return FieldFillResult{Err: err}
	}
	var candidates []formField
	for _, current := range currentFields {
		if current.Type != "file" || current.DocumentKind != f.DocumentKind || normalizeLabel(current.Question) != normalizeLabel(f.Question) || current.UploadViaButton != f.UploadViaButton {
			continue
		}
		if f.ID != "" && current.ID != f.ID {
			continue
		}
		candidates = append(candidates, current)
	}
	if len(candidates) != 1 {
		return FieldFillResult{Err: fmt.Errorf("adaptive: upload control became stale or ambiguous")}
	}
	f = candidates[0]
	lazy.applyCaps(capsFromFormFields([]formField{f}))
	if present, ambiguous := detectSiteResumeOnField(f, b.policies().ResumeMode == domain.ResumeDocumentModeSiteHosted); present {
		lazy.formCaps.SiteResumePresent = true
		lazy.formCaps.SiteResumeAmbiguous = ambiguous
	}
	resumePath, coverPath := lazy.get()
	if lazy.policyBlocked() {
		return FieldFillResult{Err: fmt.Errorf("documents: %s", lazy.holdReason)}
	}
	kind := f.DocumentKind
	if kind == "" || kind == "unknown" {
		return FieldFillResult{Err: fmt.Errorf("documents: upload purpose is unknown; review the required document")}
	}
	path := resumePath
	if kind == documents.KindCoverLetter {
		path = coverPath
	}
	if path == "" {
		if kind == documents.KindResume && f.HasFile && (lazy.useSiteResume || b.policies().ResumeMode == domain.ResumeDocumentModeSiteHosted) {
			return FieldFillResult{Filled: true}
		}
		if !f.Required {
			return FieldFillResult{Skipped: true}
		}
		return FieldFillResult{Err: fmt.Errorf("documents: required %s is unavailable", kind)}
	}
	info, infoErr := page.Context(ctx).Info()
	if infoErr != nil {
		return FieldFillResult{Err: infoErr}
	}
	destination, parseErr := url.Parse(info.URL)
	if parseErr != nil {
		return FieldFillResult{Err: parseErr}
	}
	// A SEEK discovery handoff can land at an external ATS. Choose the upload
	// adapter from the active destination rather than the original job board.
	seekDestination := trustedSeekURL(info.URL)
	if destination.Hostname() == "127.0.0.1" || destination.Hostname() == "localhost" {
		seekDestination = b.cfg.Platform == domain.PlatformSeek
	}
	switch {
	case seekDestination:
		if kind == documents.KindCoverLetter {
			err = b.seekUploadCoverLetter(page.Context(ctx), path)
		} else {
			err = b.seekUploadResume(page.Context(ctx), path)
		}
	case f.UploadViaButton:
		err = b.formUploadViaButton(ctx, page, path)
	default:
		err = uploadAdaptiveFile(ctx, page, f, path)
	}
	if err != nil {
		return FieldFillResult{Err: fmt.Errorf("documents: %s upload failed: %w", kind, err)}
	}
	return FieldFillResult{Filled: true}
}

func uploadAdaptiveFile(ctx context.Context, page *rod.Page, field formField, path string) error {
	page = page.Context(ctx)
	el, err := page.ElementByJS(rod.Eval(`(id, index) => {`+jsLinkedInApplyContainerFn+`
  const scope = resolveApplyFormContainer();
  if (!scope) return null;
  const files = documentUploadInputs(scope);
  const matches = id ? files.filter(el => el.id === id) : files.filter((_, i) => i === index);
  return matches.length === 1 ? matches[0] : null;
 }`, field.ID, field.Index))
	if err != nil {
		return fmt.Errorf("locate current upload: %w", err)
	}
	if err := el.SetFiles([]string{path}); err != nil {
		return err
	}
	loaded, err := el.Eval(`(name) => this.files && this.files.length === 1 && this.files[0].name === name && this.files[0].size > 0`, filepath.Base(path))
	if err != nil || loaded == nil || !loaded.Value.Bool() {
		return fmt.Errorf("uploaded file identity could not be verified")
	}
	return nil
}
