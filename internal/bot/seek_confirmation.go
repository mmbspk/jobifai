package bot

import (
	"net/url"
	"strings"

	"github.com/go-rod/rod"
)

// A native sent banner proves the current submission only on the expected
// job's success route and for its exact employer. Recommendations and asset
// bundles are not confirmation evidence.
func seekNativeSubmissionConfirmed(page *rod.Page, jobID, jobURL, company string) bool {
	if jobID == "" || company == "" || !trustedSeekURL(jobURL) || seekJobIDFromURL(jobURL) != jobID {
		return false
	}
	current := currentPageURL(page)
	u, err := url.Parse(current)
	if err != nil || !trustedSeekURL(current) || strings.TrimSuffix(u.Path, "/") != "/job/"+jobID+"/apply/success" {
		return false
	}
	result, err := page.Eval(jsSeekNativeSubmissionConfirmed, company)
	return err == nil && result != nil && result.Value.Bool()
}

const jsSeekNativeSubmissionConfirmed = `(company)=>{
 const visible=el=>{const r=el.getBoundingClientRect();if(r.width<2||r.height<2)return false;for(let p=el;p;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.inert||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden'||Number(s.opacity)===0)return false;}return true;};
 const norm=s=>(s||'').replace(/[\u200B-\u200D\uFEFF]/g,'').replace(/\s+/g,' ').trim().toLowerCase();
 const headings=[...document.querySelectorAll('h1#applicationSent')].filter(visible);
 if(headings.length!==1)return false;
 const heading=headings[0];
 for(let scope=heading.parentElement,depth=0;scope&&scope!==document.body&&depth<3;scope=scope.parentElement,depth++){
  if([...scope.querySelectorAll('h1,h2,h3,h4,h5,h6,[role=heading]')].some(other=>other!==heading&&visible(other)))break;
  if(scope.closest('aside,nav,[data-testid=similar-jobs-carousel]'))return false;
  const messages=[...scope.querySelectorAll('span,p,div,[role=status]')].filter(el=>visible(el)&&!el.children.length&&!el.closest('a,button,aside,nav,[data-testid=similar-jobs-carousel]')).map(el=>norm(el.textContent)).filter(text=>/^your application has been sent to\s+/.test(text));
  if(messages.length)return messages.length===1&&messages[0]==='your application has been sent to '+norm(company);
 }
 return false;
}`
