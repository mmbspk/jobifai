// Executable DOM regression checks against the production Go-embedded script.
// Run: node --test linkedin-resolver.test.cjs (from web/).
const {test}=require('node:test');
const assert=require('node:assert/strict');
const {readFileSync}=require('node:fs');
const {JSDOM}=require('jsdom');
const source=readFileSync(__dirname+'/../internal/bot/linkedin_scripts.go','utf8');
const script=source.match(/const jsLinkedInResolve = `([\s\S]*?)`\n/)[1];
const target='4479748087', other='4478417422';
function page(html,path='/jobs/view/'+target){
 const dom=new JSDOM(html,{url:'https://www.linkedin.com'+path,runScripts:'outside-only'});
 dom.window.HTMLElement.prototype.getBoundingClientRect=function(){return {width:120,height:40}};
 dom.window.HTMLElement.prototype.scrollIntoView=function(){};
 const resolve=dom.window.eval('('+script+')');
 return {dom,resolve:click=>JSON.parse(resolve(target,click))};
}
const pane=body=>`<main data-job-id="${target}"><h1>Principal Developer at MYOB</h1>${body}</main>`;
const external='<a href="https://employer.example/apply">Apply</a>';
const native='<button>Easy Apply</button>';
for(const [name,html,path,kind] of [
 ['external with another job native button',pane(external+`<section data-job-id="${other}">${native}</section>`),undefined,'external'],
 ['external with collection SDUI link',pane(external+`<a href="/jobs/collections/similar-jobs/?currentJobId=${other}&referenceJobId=${target}&openSDUIApplyFlow=true">Easy Apply</a>`),undefined,'external'],
 ['collection selects wrong job',`<main data-job-id="${other}">${native}</main>`,`/jobs/collections/similar-jobs/?currentJobId=${other}&referenceJobId=${target}`,'unknown'],
 ['matching collection and pane',pane(external),`/jobs/collections/similar-jobs/?currentJobId=${target}`,'external'],
 ['matching URL stale rendered pane',`<main data-job-id="${other}">${native}</main>`,undefined,'unknown'],
 ['native intended control',pane(native),undefined,'native'],
 ['unowned main is not evidence','<main>'+native+'</main>',undefined,'unknown'],
 ['title link proves root',`<main><h1><a href="/jobs/view/${target}">Role</a></h1>${external}</main>`,undefined,'external'],
 ['SDUI false flag is not native',pane(`<a href="/jobs/view/${target}?openSDUIApplyFlow=false">Apply</a>`),undefined,'external'],
 ['hidden native ignored',pane(external+'<button style="visibility:hidden">Easy Apply</button>'),undefined,'external'],
 ['ambiguous controls held',pane(external+native),undefined,'unknown'],
 ['wrong job submitted text ignored',pane(external+`<section data-job-id="${other}"><div role="status">Application submitted</div></section>`),undefined,'external'],
 ['body submitted text ignored',pane(external)+'<p>Application submitted</p>',undefined,'external'],
 ['verified prior application',pane('<button data-test-job-apply-button-applied>Applied</button>'),undefined,'already_applied'],
 ['foreign host native link rejected',pane(`<a href="https://evil.example/jobs/view/${target}">Easy Apply</a>`),undefined,'unknown'],
 ['recommendation heading owner rejected',pane(external+`<section><h3><a href="/jobs/view/${other}">Other role</a></h3>${native}</section>`),undefined,'external'],
]) test(name,()=>{const p=page(html,path);try{assert.equal(p.resolve(false).kind,kind);let clicks=0;p.dom.window.document.addEventListener('click',e=>{clicks++;e.preventDefault()});const r=p.resolve(true);assert.equal(r.kind,kind);assert.equal(clicks,kind==='native'?1:0);}finally{p.dom.window.close()}});
test('identity rechecked between detection and click',()=>{
 const p=page(pane(native)); assert.equal(p.resolve(false).kind,'native');
 p.dom.window.history.replaceState({},'',`/jobs/collections/similar-jobs/?currentJobId=${other}&referenceJobId=${target}`);
 let clicks=0;p.dom.window.document.addEventListener('click',()=>clicks++);
 assert.equal(p.resolve(true).identity_verified,false);assert.equal(clicks,0);p.dom.window.close();
});
