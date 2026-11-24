package bot

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
)

const jsSeekDocumentInput = `(kind)=>{` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();if(!scope)return null;
 const marker=kind==='cover_letter'?'coverLetterFileInput':'resumeFileInput';
 const slots=allInDOM(scope,'[data-testid='+marker+']');
 if(slots.length>1)return null;
 const files=slots.length?allInDOM(slots[0],'input[type=file]'):allInDOM(scope,'input[type=file]').filter(el=>{
  const label=[...scope.querySelectorAll('label[for]')].find(l=>l.htmlFor===el.id)||el.closest('label');
  const text=((label&&label.textContent)||el.getAttribute('aria-label')||'').toLowerCase();
  return kind==='cover_letter'?/cover/.test(text):/resume|résumé|resumé|\bcv\b/.test(text)&&!/cover/.test(text);
 });
 return files.length===1?files[0]:null;
}`

func seekDocumentInput(page *rod.Page, kind string) (*rod.Element, error) {
	input, err := page.Timeout(5 * time.Second).ElementByJS(rod.Eval(jsSeekDocumentInput, kind))
	if err != nil {
		return nil, fmt.Errorf("%s upload control is missing or ambiguous: %w", kind, err)
	}
	return input, nil
}

const jsSeekDocumentSelected = `(kind,filename)=>{` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();if(!scope)return {selected:false};
 const visible=el=>{const r=el.getBoundingClientRect();if(!r.width&&!r.height)return false;for(let p=el;p;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.inert||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden'||Number(s.opacity)===0)return false;}return true;};
 const norm=s=>(s||'').replace(/\s+/g,' ').trim().toLowerCase();
 const names=el=>[el,...allInDOM(el,'span,p,a,label,div,[aria-label]')].some(n=>visible(n)&&(norm(n.textContent)===filename||norm(n.getAttribute('aria-label'))===filename||norm(n.getAttribute('aria-label'))==='download '+filename));
 const label=radio=>[...scope.querySelectorAll('label[for]')].find(l=>l.htmlFor===radio.id)||radio.closest('label')||radio.parentElement;
 const marker=kind==='cover_letter'?'coverLetterFileInput':'resumeFileInput';
 const slots=allInDOM(scope,'[data-testid='+marker+']');
 if(slots.length>1)return {selected:false,ambiguous:true};
 const errors=allInDOM(scope,'[aria-invalid=true],[role=alert],[data-testid*=error]').filter(visible).some(el=>el.getAttribute('aria-invalid')==='true'||/error|fail|invalid|unable|limit|too (large|big)/i.test(el.textContent||''));
 if(!slots.length){
  const input=(` + jsSeekDocumentInput + `)(kind);
  return {selected:!!input&&!!input.files&&input.files.length===1&&norm(input.files[0].name)===filename&&input.checkValidity(),error:errors};
 }
 if(kind==='resume'){
  const matches=allInDOM(scope,'input[type=radio]').filter(r=>/^(document-select-|resume-method)/.test(r.name||'')&&names(label(r)));
  return {selected:matches.length===1&&matches[0].checked,error:errors,ambiguous:matches.length>1,radioID:matches.length===1&&!matches[0].disabled?matches[0].id:''};
 }
 const excluded=allInDOM(scope,'input[type=radio]').some(r=>/^coverLetter-method/.test(r.name||'')&&r.checked&&(r.getAttribute('data-testid')==='coverLetter-method-none'||/don.?t include/i.test((label(r)||{}).textContent||'')));
 return {selected:!excluded&&names(slots[0]),error:errors};
}`

// A completed upload may add a library entry without changing SEEK's current
// resume selection. Select only the unique exact approved filename in the
// scoped resume group, then require the usual stable attachment verification.
const jsSeekSelectApprovedResume = `(filename)=>{` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();if(!scope)return false;
 const state=(` + jsSeekDocumentSelected + `)('resume',filename);
 if(state.error||state.ambiguous||!state.radioID)return false;
 const matches=allInDOM(scope,'input[type=radio]').filter(r=>r.id===state.radioID);
 if(matches.length!==1||matches[0].disabled)return false;
 const radio=matches[0];
 if(!radio.checked)radio.click();
 return radio.checked;
}`

func (b *Bot) seekWaitDocumentSelected(page *rod.Page, kind, path string) error {
	ctx, cancel := context.WithTimeout(page.GetContext(), 20*time.Second)
	defer cancel()
	page = page.Context(ctx)
	filename := strings.ToLower(filepath.Base(path))
	stable := 0
	for ctx.Err() == nil {
		state, err := page.Eval(jsSeekDocumentSelected, kind, filename)
		if err != nil {
			return fmt.Errorf("%w: %s attachment verification: %v", errAdaptiveDocumentReview, kind, err)
		}
		if state.Value.Get("error").Bool() {
			return fmt.Errorf("%w: %s upload was rejected by the current form", errAdaptiveDocumentReview, kind)
		}
		if state.Value.Get("ambiguous").Bool() {
			return fmt.Errorf("%w: %s attachment identity is ambiguous", errAdaptiveDocumentReview, kind)
		}
		if state.Value.Get("selected").Bool() {
			stable++
		} else {
			stable = 0
			if kind == "resume" && state.Value.Get("radioID").String() != "" {
				if _, err := page.Eval(jsSeekSelectApprovedResume, filename); err != nil {
					return fmt.Errorf("%w: exact resume selection: %v", errAdaptiveDocumentReview, err)
				}
			}
		}
		if stable >= 2 {
			b.log.Info().Str("document_kind", kind).Msg("seek: approved document attachment verified")
			return nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("%w: %s uploaded file was not confirmed as the selected attachment: %v", errAdaptiveDocumentReview, kind, ctx.Err())
}

// Uploaded files can be replaced when SEEK advances to its review page. Read
// each attachment from its own visible summary block rather than treating the
// earlier upload acknowledgement or a filename elsewhere on the page as proof.
const jsSeekFinalReviewDocument = `(kind,filename)=>{` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();if(!scope)return {verified:false,reason:'summary_scope_missing'};
 const visible=el=>{const r=el.getBoundingClientRect();if(r.width<2&&r.height<2)return false;for(let p=el;p;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.inert||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden'||Number(s.opacity)===0)return false;}return true;};
 const norm=s=>(s||'').replace(/\s+/g,' ').trim().toLowerCase();
 const headings=allInDOM(scope,'h1,h2,h3,h4,h5,h6,legend,[role=heading]').filter(visible);
 const documentKind=el=>{const text=norm(el.textContent);return /^cover\s+letter\s*[:*]?$/.test(text)?'cover_letter':/^(resume|résumé|resumé|cv)\s*[:*]?$/.test(text)?'resume':'';};
 // SEEK's Documents included section uses plain spans for its row labels.
 // Recognize those only inside a unique headed document region, and never
 // climb from one row into a neighbouring document or another review section.
 const regions=headings.filter(el=>/^documents?(?:\s+included)?\s*[:*]?$/.test(norm(el.textContent))).map(el=>el.parentElement).filter(el=>el&&el!==scope&&!headings.some(other=>el.contains(other)&&!/^documents?(?:\s+included)?\s*[:*]?$/.test(norm(other.textContent))));
 const plain=regions.length===1?allInDOM(regions[0],'span,p,strong,b,dt,label').filter(el=>visible(el)&&documentKind(el)&&!el.closest('button,[role=button],a')&&!headings.some(h=>h.contains(el))&&!allInDOM(el,'span,p,strong,b,dt,label').some(child=>child!==el&&visible(child)&&documentKind(child))):[];
 const labels=[...headings.filter(documentKind),...plain];
 const named=labels.filter(el=>documentKind(el)===kind);
 if(named.length!==1)return {verified:false,reason:'document_summary_not_unique'};
 const heading=named[0];
 for(let section=heading.parentElement;section&&section!==scope;section=section.parentElement){
  if(headings.some(other=>other!==heading&&section.contains(other))||labels.some(other=>other!==heading&&section.contains(other)))break;
  const markers=allInDOM(section,'[data-testid=resume-upload],[data-testid=cover-letter-upload]').filter(visible);
  if(markers.some(el=>el.getAttribute('data-testid')!==(kind==='cover_letter'?'cover-letter-upload':'resume-upload')))break;
  const names=new Set();
  for(const el of allInDOM(section,'*')){
   if(!visible(el)||heading.contains(el)||el.matches('script,style,input,select,textarea'))continue;
   const text=norm(el.textContent);
   if(!el.children.length&&!el.closest('button,[role=button]')&&/\.(pdf|docx|doc|rtf|txt)$/.test(text))names.add(text);
   const download=norm(el.getAttribute('aria-label')).match(/^download\s+(.+\.(?:pdf|docx|doc|rtf|txt))$/);
   if(download)names.add(download[1]);
  }
  if(names.size)return {verified:names.size===1&&names.has(filename),reason:names.size===1?'attachment_read':'ambiguous_attachments'};
 }
 return {verified:false,reason:'attachment_not_found'};
}`

func seekVerifyFinalReviewDocuments(ctx context.Context, page *rod.Page, lazy *lazyDocGen) error {
	if lazy == nil {
		return nil
	}
	if lazy.policyBlocked() {
		return fmt.Errorf("%w: %s", errAdaptiveDocumentReview, lazy.holdReason)
	}
	resume, cover := lazy.peek()
	for _, doc := range []struct{ kind, path string }{{"resume", resume}, {"cover_letter", cover}} {
		if doc.path == "" {
			continue
		}
		state, err := page.Context(ctx).Timeout(5*time.Second).Eval(jsSeekFinalReviewDocument, doc.kind, strings.ToLower(filepath.Base(doc.path)))
		if err != nil {
			return fmt.Errorf("%w: %s final-review verification: %v", errAdaptiveDocumentReview, doc.kind, err)
		}
		if !state.Value.Get("verified").Bool() {
			return fmt.Errorf("%w: approved %s is not uniquely confirmed in the final review (%s)", errAdaptiveDocumentReview, doc.kind, state.Value.Get("reason").String())
		}
	}
	return nil
}
