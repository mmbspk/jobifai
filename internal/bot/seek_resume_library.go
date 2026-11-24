package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/user/jobifai/internal/domain"
)

var errSeekResumeLibraryFull = errors.New("SEEK saved-resume library is full")

const seekResumeUsePrefix = "seek_resume_last_use:"

// The live SEEK dialog supplies document IDs and added dates. Its order is not
// a last-use signal. Prefer explicit last-use metadata and our ID-scoped usage
// records; bootstrap untracked saved documents from their displayed added date.
type seekResumeLibraryRow struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	LastUsed string `json:"last_used"`
	Selected bool   `json:"selected"`
	Default  bool   `json:"default"`
}

type seekResumeLibraryState struct {
	Present   bool                   `json:"present"`
	Verified  bool                   `json:"verified"`
	Reason    string                 `json:"reason"`
	Signature string                 `json:"signature"`
	Rows      []seekResumeLibraryRow `json:"rows"`
}

type seekResumeUse struct {
	Filename string    `json:"filename"`
	UsedAt   time.Time `json:"used_at"`
}

const jsSeekResumeLibraryHelpers = jsLinkedInApplyContainerFn + `
 const libraryVisible=el=>{if(!el)return false;const r=el.getBoundingClientRect();if(r.width<2&&r.height<2)return false;for(let p=el;p;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.inert||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden'||s.opacity==='0')return false;}return true;};
 const libraryNorm=s=>String(s||'').replace(/[\u2060\uFEFF\u200B-\u200D\u00AD]/g,'').replace(/\s+/g,' ').trim();
 const libraryRadios=scope=>allInDOM(scope,'input[type=radio]').filter(r=>/^(document-select-|resume-method)/.test(r.name||'')&&r.value&&libraryVisible(libraryRadioLabel(scope,r))&&!!linkedInResumeFilename(libraryRadioLabel(scope,r).textContent));
 const libraryRadioLabel=(scope,r)=>allInDOM(scope,'label[for]').find(label=>label.htmlFor===r.id)||r.closest('label')||r.parentElement;
 const libraryDialog=()=>{const markers=allInDOM(document,'[data-testid="10-resume-limit-text"],#docLimitExceededDropdown').filter(libraryVisible);if(!markers.length)return null;const dialogs=[...new Set(markers.map(el=>el.closest('dialog,[role=dialog]')).filter(Boolean))];return dialogs.length===1&&markers.every(el=>dialogs[0].contains(el))?dialogs[0]:null;};
 // A capacity dialog is an account overlay, not the application form. The
 // general LinkedIn role-dialog fallback may identify it while it is open.
 // Bind the underlying native SEEK application through its owned form marker
 // or the same verified native navigation/heading relationship instead.
 const libraryApplyScope=()=>{
  const dialog=libraryDialog(),original=resolveApplyContainer();
  if(original&&(!dialog||(!dialog.contains(original)&&original!==dialog)))return original;
  for(const selector of ['[data-testid="quick-apply-form"]','[data-automation="apply-form"]','form[data-testid*="applicationForm"]']){const forms=allInDOM(document,selector).filter(el=>libraryVisible(el)&&(!dialog||!dialog.contains(el)));if(forms.length>1)return null;if(forms.length===1)return forms[0];}
  if(!isNativeSeekApplyPage())return null;
  const mains=allInDOM(document,'main,[role=main]').filter(el=>libraryVisible(el)&&(!dialog||!dialog.contains(el)));if(mains.length>1)return null;
  const root=mains[0]||document,nav=allInDOM(root,'button[data-testid="continue-button"],button[data-testid="review-submit-application"],[data-automation="review-submit-button"],button[data-testid="review-submit-button"],button[data-testid="submit-application"]').filter(el=>libraryVisible(el)&&(!dialog||!dialog.contains(el)));
  if(nav.length!==1)return null;if(mains.length===1)return mains[0];
  let headings=allInDOM(document,'h1').filter(el=>libraryVisible(el)&&(!dialog||!dialog.contains(el)));if(!headings.length)headings=allInDOM(document,'h2').filter(el=>libraryVisible(el)&&(!dialog||!dialog.contains(el)));if(headings.length!==1)return null;
  for(let parent=nav[0].parentElement;parent&&parent!==document.body&&parent!==document.documentElement;parent=parent.parentElement){if(!parent.contains(headings[0]))continue;if(allInDOM(parent,'header,nav,[role=banner],[role=navigation]').some(el=>el.querySelector('input,select,textarea,[contenteditable=true]')))return null;return parent;}
  return null;
 };
 const libraryState=()=>{
  const markers=allInDOM(document,'[data-testid="10-resume-limit-text"],#docLimitExceededDropdown').filter(libraryVisible);
  if(!markers.length)return {present:false};
  const dialog=libraryDialog(),scope=libraryApplyScope();
  if(!dialog||!scope)return {present:true,verified:false,reason:'limit_dialog_not_unique'};
  const selects=allInDOM(dialog,'select#docLimitExceededDropdown').filter(libraryVisible),messages=allInDOM(dialog,'[data-testid="10-resume-limit-text"]').filter(libraryVisible),buttons=allInDOM(dialog,'button[data-automation="10-resume-delete"]').filter(libraryVisible);
  if(selects.length!==1||messages.length!==1||buttons.length!==1||!/^delete(?: (?:selected )?(?:resume|resumé|résumé|cv))?$/i.test(libraryNorm(buttons[0].textContent)))return {present:true,verified:false,reason:'limit_controls_not_unique'};
  const radioList=libraryRadios(scope),rows=[],ids=new Set();
  const options=[...selects[0].options].filter(option=>option.value&&!option.disabled);
  if(!options.length||options.length>50)return {present:true,verified:false,reason:'limit_choices_out_of_bounds'};
  for(const option of options){
   if(ids.has(option.value)||option.value.length>200)return {present:true,verified:false,reason:'duplicate_document_identity'};
   ids.add(option.value);
   const owned=radioList.filter(r=>r.value===option.value);
   if(owned.length!==1)return {present:true,verified:false,reason:'saved_document_not_unique'};
   const radio=owned[0],label=libraryRadioLabel(scope,radio),labelText=libraryNorm(label&&label.textContent).toLowerCase(),optionText=libraryNorm(option.textContent);
   const separator=optionText.indexOf(' - ');
   if(separator<0||optionText.slice(separator+3).toLowerCase()!==labelText)return {present:true,verified:false,reason:'saved_document_label_mismatch'};
   let isDefault=option.dataset.default==='true'||radio.dataset.default==='true';
   for(let row=radio.parentElement,depth=0;row&&row!==scope&&depth<8;row=row.parentElement,depth++){if(radioList.filter(r=>row.contains(r)).length!==1)break;if(/\bdefault\b/i.test(libraryNorm(row.textContent)))isDefault=true;}
   rows.push({id:option.value,label:libraryNorm(option.textContent),last_used:option.getAttribute('data-last-used-at')||option.getAttribute('data-last-used')||radio.getAttribute('data-last-used-at')||'',selected:radio.checked||radio.getAttribute('aria-checked')==='true',default:isDefault});
   if(!label||!libraryNorm(label.textContent))return {present:true,verified:false,reason:'saved_document_label_missing'};
  }
  return {present:true,verified:true,rows,signature:JSON.stringify(rows)};
 };
`

const jsSeekResumeLibraryState = `()=>{` + jsSeekResumeLibraryHelpers + `return libraryState();}`

const jsSeekResumeLibrarySelect = `(signature,id)=>{` + jsSeekResumeLibraryHelpers + `
 const state=libraryState(),dialog=libraryDialog();if(!state.verified||state.signature!==signature||!state.rows.some(row=>row.id===id)||!dialog)return false;
 const select=allInDOM(dialog,'select#docLimitExceededDropdown').filter(libraryVisible)[0];
 select.value=id;select.dispatchEvent(new Event('input',{bubbles:true}));select.dispatchEvent(new Event('change',{bubbles:true}));
 return select.value===id;
}`

const jsSeekResumeLibraryDelete = `(signature,id,element)=>{` + jsSeekResumeLibraryHelpers + `
 const state=libraryState(),dialog=libraryDialog();if(!state.verified||state.signature!==signature||!dialog)return element?null:false;
 const selects=allInDOM(dialog,'select#docLimitExceededDropdown').filter(libraryVisible);
 if(selects.length!==1||selects[0].value!==id)return element?null:false;
 const buttons=allInDOM(dialog,'button[data-automation="10-resume-delete"]').filter(button=>libraryVisible(button)&&!button.disabled&&button.getAttribute('aria-disabled')!=='true');
 return element?(buttons.length===1?buttons[0]:null):buttons.length===1;
}`

const jsSeekResumeLibraryDeleted = `(beforeIDs,deletedID)=>{` + jsSeekResumeLibraryHelpers + `
 const scope=libraryApplyScope();if(!scope||allInDOM(document,'[data-testid="10-resume-limit-text"],#docLimitExceededDropdown').some(libraryVisible))return false;
 const remaining=libraryRadios(scope).map(r=>r.value).filter(value=>value!=='upload'&&value!=='none');
 const expected=beforeIDs.filter(id=>id!==deletedID);
 return remaining.length===expected.length&&new Set(remaining).size===remaining.length&&!remaining.includes(deletedID)&&expected.every(id=>remaining.includes(id));
}`

func seekLibraryRowDetails(row seekResumeLibraryRow) (string, time.Time, error) {
	split := strings.SplitN(row.Label, " - ", 2)
	if len(split) != 2 || strings.TrimSpace(split[1]) == "" {
		return "", time.Time{}, fmt.Errorf("saved resume added date or filename is missing")
	}
	date := strings.TrimSpace(split[0])
	var added time.Time
	var err error
	for _, layout := range []string{"2/1/06", "2/1/2006", "2006-01-02"} {
		added, err = time.Parse(layout, date)
		if err == nil {
			break
		}
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("saved resume added date is invalid")
	}
	return strings.ToLower(strings.TrimSpace(split[1])), added, nil
}

func chooseSeekResumeEviction(rows []seekResumeLibraryRow, approved string, uses map[string]seekResumeUse, history map[string]time.Time) (seekResumeLibraryRow, string, error) {
	type candidate struct {
		row           seekResumeLibraryRow
		recent, added time.Time
		source        string
	}
	var candidates []candidate
	approved = strings.ToLower(filepath.Base(approved))
	seenIDs := map[string]bool{}
	for _, row := range rows {
		if row.ID == "" || seenIDs[row.ID] {
			return seekResumeLibraryRow{}, "", fmt.Errorf("saved resume identity is missing or duplicated")
		}
		seenIDs[row.ID] = true
		if row.Selected || row.Default {
			continue
		}
		filename, added, err := seekLibraryRowDetails(row)
		if err != nil {
			return seekResumeLibraryRow{}, "", err
		}
		if filename == approved {
			continue
		}
		recent, source := added, "saved_date_fallback"
		if row.LastUsed != "" {
			used, err := time.Parse(time.RFC3339, row.LastUsed)
			if err != nil {
				return seekResumeLibraryRow{}, "", fmt.Errorf("saved resume last-use timestamp is invalid")
			}
			if used.After(recent) {
				recent = used
			}
			source = "site_last_use"
		}
		if use, ok := uses[row.ID]; ok && !use.UsedAt.IsZero() {
			if use.UsedAt.After(recent) {
				recent = use.UsedAt
			}
			source = "recorded_document_use"
		}
		// A filename-only historical use can protect every duplicate carrying that
		// name, but cannot identify which duplicate was used. Never attribute a
		// duplicate's use to one arbitrarily chosen saved document.
		if used := history[filename]; used.After(recent) {
			recent = used
			source = "historical_filename_protection"
		}
		candidates = append(candidates, candidate{row: row, recent: recent, added: added, source: source})
	}
	if len(candidates) == 0 {
		return seekResumeLibraryRow{}, "", fmt.Errorf("no saved resume can be evicted without removing a protected attachment")
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].recent.Equal(candidates[j].recent) {
			return candidates[i].recent.Before(candidates[j].recent)
		}
		if !candidates[i].added.Equal(candidates[j].added) {
			return candidates[i].added.Before(candidates[j].added)
		}
		return candidates[i].row.ID < candidates[j].row.ID
	})
	return candidates[0].row, candidates[0].source, nil
}

func (b *Bot) seekResumeUses(ctx context.Context) (map[string]seekResumeUse, map[string]time.Time, error) {
	uses, history := map[string]seekResumeUse{}, map[string]time.Time{}
	if b.cfg.DB == nil || b.cfg.UserID == "" {
		return uses, history, nil
	}
	rows, err := b.cfg.DB.QueryContext(ctx, "SELECT key,value FROM settings WHERE user_id=? AND substr(key,1,?)=?", b.cfg.UserID, len(seekResumeUsePrefix), seekResumeUsePrefix)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var key, raw string
		if err = rows.Scan(&key, &raw); err != nil {
			break
		}
		var use seekResumeUse
		if err = json.Unmarshal([]byte(raw), &use); err != nil {
			break
		}
		uses[strings.TrimPrefix(key, seekResumeUsePrefix)] = use
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, nil, err
	}
	if rowsErr != nil {
		return nil, nil, rowsErr
	}
	rows, err = b.cfg.DB.QueryContext(ctx, "SELECT COALESCE(resume_path,''),applied_at FROM jobs_applied WHERE user_id=? AND platform='seek'", b.cfg.UserID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var path, raw string
		if err = rows.Scan(&path, &raw); err != nil {
			return nil, nil, err
		}
		if path == "" {
			continue
		}
		var used time.Time
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
			if used, err = time.Parse(layout, raw); err == nil {
				break
			}
		}
		if err != nil {
			continue
		}
		name := strings.ToLower(filepath.Base(path))
		if used.After(history[name]) {
			history[name] = used
		}
	}
	return uses, history, rows.Err()
}

// Delete at most one identified saved resume per upload recovery. The modal
// closing alone is insufficient: exactly that ID must disappear and every
// other saved ID must remain before another chooser can be activated.
func (b *Bot) seekResumeLibraryGuard() error {
	if err := b.checkAdaptiveSideEffect(); err != nil {
		return err
	}
	mode := b.cfg.EngineMode
	if mode == "" {
		mode = domain.ApplyEngineLegacy
	}
	return killSwitchCheck(b.cfg.EngineStore, &AttemptContext{UserID: b.cfg.UserID, Platform: string(domain.PlatformSeek), Engine: mode})
}

func (b *Bot) seekRecoverResumeLibrary(page *rod.Page, approved string) (bool, error) {
	if strings.TrimSpace(approved) == "" {
		return false, fmt.Errorf("%w: approved resume identity is missing", errAdaptiveDocumentReview)
	}
	ctx, cancel := context.WithTimeout(page.GetContext(), 12*time.Second)
	defer cancel()
	page = page.Context(ctx)
	raw, err := page.Eval(jsSeekResumeLibraryState)
	if err != nil {
		return false, err
	}
	var state seekResumeLibraryState
	if err = json.Unmarshal([]byte(raw.Value.JSON("", "")), &state); err != nil {
		return false, err
	}
	if !state.Present {
		return false, nil
	}
	if !state.Verified {
		return false, fmt.Errorf("%w: SEEK resume limit recovery lacks a unique owned library (%s)", errAdaptiveDocumentReview, state.Reason)
	}
	uses, history, err := b.seekResumeUses(ctx)
	if err != nil {
		return false, fmt.Errorf("%w: read saved-resume usage: %v", errAdaptiveDocumentReview, err)
	}
	target, source, err := chooseSeekResumeEviction(state.Rows, approved, uses, history)
	if err != nil {
		return false, fmt.Errorf("%w: %v", errAdaptiveDocumentReview, err)
	}
	if err = b.seekResumeLibraryGuard(); err != nil {
		return false, err
	}
	selected, err := page.Eval(jsSeekResumeLibrarySelect, state.Signature, target.ID)
	if err != nil || selected == nil || !selected.Value.Bool() {
		return false, fmt.Errorf("%w: saved-resume deletion selection changed", errAdaptiveDocumentReview)
	}
	readyDeadline := time.Now().Add(2 * time.Second)
	for {
		current, currentErr := page.Eval(jsSeekResumeLibraryState)
		if currentErr != nil || current == nil || !current.Value.Get("verified").Bool() || current.Value.Get("signature").String() != state.Signature {
			return false, fmt.Errorf("%w: saved-resume library changed after deletion selection", errAdaptiveDocumentReview)
		}
		ready, readyErr := page.Eval(jsSeekResumeLibraryDelete, state.Signature, target.ID, false)
		if readyErr != nil {
			return false, readyErr
		}
		if ready != nil && ready.Value.Bool() {
			break
		}
		if time.Now().After(readyDeadline) || ctx.Err() != nil {
			return false, fmt.Errorf("%w: selected saved-resume delete control did not become ready", errAdaptiveDocumentReview)
		}
		select {
		case <-ctx.Done():
		case <-time.After(50 * time.Millisecond):
		}
	}
	button, err := page.ElementByJS(rod.Eval(jsSeekResumeLibraryDelete, state.Signature, target.ID, true))
	if err != nil {
		return false, fmt.Errorf("%w: selected saved-resume delete control unavailable: %v", errAdaptiveDocumentReview, err)
	}
	if _, err = button.Eval(`()=>this.scrollIntoView({block:'nearest',behavior:'instant'})`); err != nil {
		return false, err
	}
	point, err := adaptiveTypeaheadClickPoint(ctx, page, button)
	if err != nil {
		return false, err
	}
	if err = moveBoundedMouse(ctx, page, *point); err != nil {
		return false, err
	}
	fresh, err := button.Eval(`(signature,id)=>this===(`+jsSeekResumeLibraryDelete+`)(signature,id,true)`, state.Signature, target.ID)
	if err != nil || fresh == nil || !fresh.Value.Bool() {
		return false, fmt.Errorf("%w: saved-resume delete action changed before dispatch", errAdaptiveDocumentReview)
	}
	if err = b.seekResumeLibraryGuard(); err != nil {
		return false, err
	}
	point, err = adaptiveTypeaheadClickPoint(ctx, page, button)
	if err != nil {
		return false, err
	}
	if err = b.seekResumeLibraryGuard(); err != nil {
		return false, err
	}
	if !page.IsIframe() {
		point, err = probeLinkedInNativeClickPoint(ctx, button)
		if err != nil {
			return false, err
		}
	}
	fresh, err = button.Eval(`(signature,id)=>this===(`+jsSeekResumeLibraryDelete+`)(signature,id,true)`, state.Signature, target.ID)
	if err != nil || fresh == nil || !fresh.Value.Bool() {
		return false, fmt.Errorf("%w: saved-resume selection changed after visibility settling", errAdaptiveDocumentReview)
	}
	if err = b.seekResumeLibraryGuard(); err != nil {
		return false, err
	}
	if err = clickBoundedMouse(ctx, page, *point); err != nil {
		return false, fmt.Errorf("%w: saved-resume deletion dispatch uncertain: %v", errAdaptiveDocumentReview, err)
	}
	ids := make([]string, len(state.Rows))
	for i, row := range state.Rows {
		ids[i] = row.ID
	}
	for ctx.Err() == nil {
		ack, err := page.Eval(jsSeekResumeLibraryDeleted, ids, target.ID)
		if err != nil {
			return false, fmt.Errorf("%w: saved-resume deletion acknowledgement: %v", errAdaptiveDocumentReview, err)
		}
		if ack != nil && ack.Value.Bool() {
			if b.cfg.DB != nil && b.cfg.UserID != "" {
				_, _ = b.cfg.DB.ExecContext(ctx, "DELETE FROM settings WHERE user_id=? AND key=?", b.cfg.UserID, seekResumeUsePrefix+target.ID)
			}
			b.log.Info().Str("ranking_source", source).Int("remaining_saved_resumes", len(ids)-1).Msg("seek: saved-resume capacity recovered and deletion verified")
			return true, nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}
	return false, fmt.Errorf("%w: saved-resume deletion was not acknowledged; no further deletion or upload attempted", errAdaptiveDocumentReview)
}

func (b *Bot) seekRecordResumeUse(page *rod.Page, path string) {
	if b.cfg.DB == nil || b.cfg.UserID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(page.GetContext(), 2*time.Second)
	defer cancel()
	filename := strings.ToLower(filepath.Base(path))
	value, err := page.Context(ctx).Eval(`(filename)=>{`+jsSeekResumeLibraryHelpers+`const scope=libraryApplyScope();if(!scope)return '';const rows=libraryRadios(scope).filter(r=>r.checked&&linkedInResumeFilename(libraryRadioLabel(scope,r).textContent)===filename);return rows.length===1?rows[0].value:'';}`, filename)
	if err != nil || value == nil || value.Value.String() == "" {
		return
	}
	id := value.Value.String()
	if len(id) > 200 {
		return
	}
	raw, err := json.Marshal(seekResumeUse{Filename: filename, UsedAt: time.Now().UTC()})
	if err != nil {
		return
	}
	_, err = b.cfg.DB.ExecContext(ctx, `INSERT INTO settings(user_id,key,value) VALUES(?,?,?) ON CONFLICT(user_id,key) DO UPDATE SET value=excluded.value`, b.cfg.UserID, seekResumeUsePrefix+id, string(raw))
	if err != nil {
		b.log.Warn().Err(err).Msg("seek: saved-resume last-use record could not be persisted")
	}
}
