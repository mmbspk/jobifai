// Executable DOM regression checks against the production Go-embedded script.
// Run: node --test linkedin-resolver-check.cjs (from web/).
const {test}=require('node:test');
const assert=require('node:assert/strict');
const {readFileSync}=require('node:fs');
const {JSDOM}=require('jsdom');
const source=readFileSync(__dirname+'/../internal/bot/linkedin_scripts.go','utf8');
const script=source.match(/const jsLinkedInResolve = `([\s\S]*?)`\n/)[1];
const confirmation=source.match(/const jsLinkedInSubmissionConfirmed = `([\s\S]*?\n})`/)[1].replace('` + jsLinkedInResolve + `',script);
const target='4479748087', other='4478417422';
function page(html,path='/jobs/view/'+target,expectedID=target){
 const dom=new JSDOM(html,{url:'https://www.linkedin.com'+path,runScripts:'outside-only'});
 dom.window.HTMLElement.prototype.getBoundingClientRect=function(){return {width:120,height:40}};
 dom.window.HTMLElement.prototype.scrollIntoView=function(){};
 const resolve=dom.window.eval('('+script+')');
 const confirm=dom.window.eval('('+confirmation+')');
 return {dom,resolve:(click,metadata={})=>JSON.parse(resolve(expectedID,click,false,metadata.title||'',metadata.company||'')),confirm:company=>confirm(expectedID,company)};
}
for(const [name,id,kind,description] of [
 ['external','4479800927','external','Project Delivery:'],
 ['native','4477930880','native','Australian citizenship'],
]) {
 const html=readFileSync(__dirname+'/../internal/bot/testdata/linkedin/sdui-'+name+'.html','utf8');
 test('captured full SDUI '+name+' pane classifies and extracts its job description',()=>{
  const p=page(html,'/jobs/view/'+id,id);
  try {
   const recommendation=p.dom.window.document.querySelector('[id^="JobDetailsSimilarJobsSlot_"]');
   recommendation.insertAdjacentHTML('beforeend','<button>Easy Apply</button><a href="https://wrong.example/apply">Apply</a>');
   let clicks=0;p.dom.window.document.addEventListener('click',e=>{clicks++;e.preventDefault()});
   const r=p.resolve(false);assert.equal(r.kind,kind);assert.equal(r.identity_verified,true);
   assert.ok(r.description.includes(description));assert.ok(r.description.length>1000);assert.ok(!r.description.includes('More jobs'));
   const clicked=p.resolve(true);assert.equal(clicked.kind,kind);assert.equal(clicks,kind==='native'?1:0);
  }finally{p.dom.window.close()}
 });
 for(const [mutation,change] of [
  ['description belongs to another job',d=>d.querySelector('[id^="JobDetails_AboutTheJob_"]').id='JobDetails_AboutTheJob_999'],
  ['header belongs to another job',d=>d.querySelector('[id^="JobDetails_ManageJobBanner_"]').nextElementSibling.setAttribute('data-job-id','999')],
  ['header job link disagrees',d=>d.querySelector('[id^="JobDetails_ManageJobBanner_"]').nextElementSibling.querySelector('a[href*="/jobs/view/"]').href='/jobs/view/999'],
  ['duplicate detail columns',d=>{const c=d.querySelector('[data-testid="lazy-column"]');c.after(c.cloneNode(true))}],
 ]) test('captured SDUI '+name+' rejects '+mutation,()=>{
  const p=page(html,'/jobs/view/'+id,id);try{change(p.dom.window.document);let clicks=0;p.dom.window.document.addEventListener('click',()=>clicks++);const r=p.resolve(true);assert.equal(r.identity_verified,false);assert.equal(r.action,'');assert.equal(r.description,'');assert.equal(clicks,0);}finally{p.dom.window.close()}
 });
 test('captured SDUI '+name+' rejects another selected collection job',()=>{
  const p=page(html,'/jobs/collections/similar-jobs/?currentJobId=999&referenceJobId='+id,id);try{const r=p.resolve(true);assert.equal(r.identity_verified,false);assert.equal(r.action,'');assert.equal(r.description,'');}finally{p.dom.window.close()}
 });
 test('captured SDUI '+name+' does not depend on generated wrapper classes',()=>{
  const p=page(html,'/jobs/view/'+id,id);
  try {p.dom.window.document.querySelectorAll('[class]').forEach(el=>el.removeAttribute('class'));assert.equal(p.resolve(false).kind,kind);}finally{p.dom.window.close()}
 });
 test('captured SDUI '+name+' rejects conflicting declared listing metadata',()=>{
  const p=page(html,'/jobs/view/'+id,id);
  try {
   for(const metadata of [{title:'Unrelated different role'},{company:'Unrelated different employer'}]) {
    const r=p.resolve(true,metadata);assert.equal(r.identity_verified,false);assert.equal(r.reason,'listing_metadata_mismatch');assert.equal(r.action,'');assert.equal(r.description,'');
   }
  }finally{p.dom.window.close()}
 });
 test('captured SDUI '+name+' never widens the header to a page-level recommendation',()=>{
  const p=page(html,'/jobs/view/'+id,id);
  try {
   const d=p.dom.window.document;
   d.querySelector('[aria-label="Easy Apply to this job"], [aria-label="Apply on company website"]').remove();
   const rec=d.createElement('div');rec.innerHTML='<button>Easy Apply</button>';
   d.querySelector('[id^="JobDetails_AboutTheJob_"]').after(rec);
   let clicks=0;d.addEventListener('click',()=>clicks++);
   const r=p.resolve(true);assert.equal(r.kind,'unknown');assert.equal(r.action,'');assert.equal(clicks,0);
  }finally{p.dom.window.close()}
 });
}
const pane=body=>`<main data-job-id="${target}"><h1>Principal Developer at MYOB</h1>${body}</main>`;
const external='<a href="https://employer.example/apply">Apply</a>';
const native='<button>Easy Apply</button>';
// Captured SDUI control shapes supplied by the user. The verified parent below
// is synthetic: the capture contains the controls, not their job-header owner.
const sduiNative='<div class="iiimki iiiax0 iiiifh iiijmn iiijl2 iiijmd iiiih5 iiimkb"><div class="iiia1c iiihex iiihu9 iiimkb"><button type="button" componentkey="fcbdc6af-86ae-4f4e-a042-6ee86f5e204f" aria-label="Easy Apply to this job"><span><svg aria-hidden="true"></svg><span>Easy Apply</span></span></button></div><button type="button" aria-label="Save the job"><span>Save</span></button></div>';
const sduiExternal='<div class="iiimki iiiax0 iiiifh iiijmn iiijl2 iiijmd iiiih5 iiimkb"><div class="iiia1c iiihex iiihu9 iiimkb"><a aria-disabled="false" href="https://www.linkedin.com/safety/go/?url=https%3A%2F%2Fjobs.lever.co%2FZeller%2F0fbec193-ea8a-4c5d-8441-d4ad3dab6ed2%2Fapply&isSdui=true" target="_blank" componentkey="ec093e15-bea5-4a74-a698-9a07620fc5de" aria-label="Apply on company website"><span><svg aria-hidden="true"></svg><span>Apply</span></span></a></div><button type="button" aria-label="Save the job"><span>Save</span></button></div>';
for(const [name,html,path,kind] of [
 ['captured SDUI native control in verified owner',pane(sduiNative),undefined,'native'],
 ['captured SDUI safety redirect control in verified owner',pane(sduiExternal),undefined,'external'],
 ['external with another job native button',pane(external+`<section data-job-id="${other}">${native}</section>`),undefined,'external'],
 ['external with collection SDUI link',pane(external+`<a href="/jobs/collections/similar-jobs/?currentJobId=${other}&referenceJobId=${target}&openSDUIApplyFlow=true">Easy Apply</a>`),undefined,'external'],
 ['collection selects wrong job',`<main data-job-id="${other}">${native}</main>`,`/jobs/collections/similar-jobs/?currentJobId=${other}&referenceJobId=${target}`,'unknown'],
 ['collection is not an individual job page',pane(external),`/jobs/collections/similar-jobs/?currentJobId=${target}`,'unknown'],
 ['matching URL stale rendered pane',`<main data-job-id="${other}">${native}</main>`,undefined,'unknown'],
 ['native intended control',pane(native),undefined,'native'],
 ['unowned main is not evidence','<main>'+native+'</main>',undefined,'unknown'],
 ['title link proves root',`<main><h1><a href="/jobs/view/${target}">Role</a></h1>${external}</main>`,undefined,'external'],
 ['job posting URN identifies root',`<main data-entity-urn="urn:li:fsd_jobPosting:${target}"><h1><a href="/jobs/view/${target}">Role</a></h1>${native}</main>`,undefined,'native'],
 ['unrelated entity URN does not hide title evidence',`<main data-entity-urn="urn:li:organization:555"><h1><a href="/jobs/view/${target}">Role</a></h1>${external}</main>`,undefined,'external'],
 ['SDUI false flag is not native',pane(`<a href="/jobs/view/${target}?openSDUIApplyFlow=false">Apply</a>`),undefined,'external'],
 ['hidden native ignored',pane(external+'<button style="visibility:hidden">Easy Apply</button>'),undefined,'external'],
 ['ambiguous controls held',pane(external+native),undefined,'unknown'],
 ['duplicate native controls held',pane(native+native),undefined,'unknown'],
 ['wrong job submitted text ignored',pane(external+`<section data-job-id="${other}"><div role="status">Application submitted</div></section>`),undefined,'external'],
 ['body submitted text ignored',pane(external)+'<p>Application submitted</p>',undefined,'external'],
 ['verified prior application',pane('<button data-test-job-apply-button-applied>Applied</button>'),undefined,'already_applied'],
 ['foreign host native link rejected',pane(`<a href="https://evil.example/jobs/view/${target}">Easy Apply</a>`),undefined,'unknown'],
 ['recommendation heading owner rejected',pane(external+`<section><h3><a href="/jobs/view/${other}">Other role</a></h3>${native}</section>`),undefined,'external'],
]) test(name,()=>{const p=page(html,path);try{assert.equal(p.resolve(false).kind,kind);let clicks=0;p.dom.window.document.addEventListener('click',e=>{clicks++;e.preventDefault()});const r=p.resolve(true);assert.equal(r.kind,kind);assert.equal(clicks,kind==='native'?1:0);}finally{p.dom.window.close()}});
test('captured SDUI controls alone do not prove pane ownership',()=>{
 for(const control of [sduiNative,sduiExternal]) {
  const p=page('<main>'+control+'</main>');
  try {const r=p.resolve(false);assert.equal(r.reason,'job_pane_not_unique');assert.equal(r.identity_verified,false);}finally{p.dom.window.close()}
 }
});
test('identity rechecked between detection and click',()=>{
 const p=page(pane(native)); assert.equal(p.resolve(false).kind,'native');
 p.dom.window.history.replaceState({},'',`/jobs/collections/similar-jobs/?currentJobId=${other}&referenceJobId=${target}`);
 let clicks=0;p.dom.window.document.addEventListener('click',()=>clicks++);
 assert.equal(p.resolve(true).identity_verified,false);assert.equal(clicks,0);p.dom.window.close();
});
test('completion dialog requires original job and employer',()=>{
 const p=page(pane(native)+'<div role="dialog">Your application was sent to OtherCo</div>');
 assert.equal(p.confirm('MYOB'),false);
 p.dom.window.document.querySelector('[role="dialog"]').textContent='Your application was sent to MYOB';
 assert.equal(p.confirm('MYOB'),true);
 p.dom.window.history.replaceState({},'',`/jobs/collections/similar-jobs/?currentJobId=${other}&referenceJobId=${target}`);
 assert.equal(p.confirm('MYOB'),false);p.dom.window.close();
});
test('confirmation can replace the job header on the original job URL',()=>{
 const p=page('<div role="dialog">Your application was sent to MYOB</div>');
 try {assert.equal(p.confirm('MYOB'),true);assert.equal(p.confirm('OtherCo'),false);}finally{p.dom.window.close()}
});
test('confirmation on a separate success route needs an explicit matching job ID',()=>{
 const p=page('<div role="dialog">Your application was sent to MYOB</div>','/jobs/application/success');
 try {assert.equal(p.confirm('MYOB'),false);p.dom.window.document.querySelector('[role="dialog"]').setAttribute('data-job-id',target);assert.equal(p.confirm('MYOB'),true);}finally{p.dom.window.close()}
});
