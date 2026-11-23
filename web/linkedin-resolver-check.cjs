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

const sduiHeader = id => '<div id="'+id+'"><a href="/jobs/view/'+target+'">Software Engineer</a><span aria-label="Company, Example.">Example</span><button>Easy Apply</button></div>';
const sduiDescription = '<div id="JobDetails_AboutTheJob_'+target+'">Intended description with enough content for extraction.</div>';
// Reduced standalone layout: explicit component IDs, plain heading, repeated
// company/logo links, no linked job title. Never use private live captures.
const standaloneHeader = '<div class="primary"><div><h2>Senior Coordinator</h2><div><a aria-label="Company, Acme." href="/company/acme">Acme</a><a aria-label="Company, Acme." href="/company/acme">Logo</a></div><div><button>Easy Apply</button></div></div></div>';
const standalone = '<div class="owner"><div id="JobDetails_ManageJobBanner_'+target+'"></div>'+standaloneHeader+'<section>'+sduiDescription+'<aside><h2>Other opportunity</h2><button>Easy Apply</button></aside></section></div>';
test('standalone SDUI verifies a plain heading using unique job component IDs',()=>{
 const p=page(standalone);
 try {let clicks=0;p.dom.window.document.querySelector('.primary button').onclick=()=>clicks++;
  const r=p.resolve(true,{title:'Senior Coordinator',company:'Acme'});
  assert.equal(r.identity_verified,true);assert.equal(r.kind,'native');assert.equal(r.company,'Acme');
  assert.ok(r.description.includes('Intended description'));assert.equal(clicks,1);
 }finally{p.dom.window.close()}
});
test('standalone SDUI verifies an exact expected paragraph title',()=>{
 const p=page(standalone.replace('<h2>Senior Coordinator</h2>','<p>Senior Coordinator</p>'));
 try {const r=p.resolve(false,{title:'Senior Coordinator',company:'Acme'});assert.equal(r.identity_verified,true);assert.equal(r.kind,'native');}
 finally{p.dom.window.close()}
});
test('standalone SDUI identity guard can recheck explicit component ownership without title metadata',()=>{
 const p=page(standalone.replace('<h2>Senior Coordinator</h2>','<p>Senior Coordinator</p>'));
 try {const r=p.resolve(false);assert.equal(r.identity_verified,true);assert.equal(r.kind,'native');}
 finally{p.dom.window.close()}
});
for(const [name,change] of [
 ['wrong banner ID',d=>d.querySelector('[id^="JobDetails_ManageJobBanner_"]').id='JobDetails_ManageJobBanner_'+other],
 ['wrong description ID',d=>d.querySelector('[id^="JobDetails_AboutTheJob_"]').id='JobDetails_AboutTheJob_'+other],
 ['conflicting component ID',d=>d.querySelector('.primary').insertAdjacentHTML('beforeend','<div id="JobDetails_ResumeReview_'+other+'"></div>')],
 ['conflicting header job ID',d=>d.querySelector('.primary').setAttribute('data-job-id',other)],
 ['conflicting job link',d=>d.querySelector('.primary').insertAdjacentHTML('beforeend','<a href="/jobs/view/'+other+'">Another listing</a>')],
 ['duplicate owners',d=>d.querySelector('.owner').after(d.querySelector('.owner').cloneNode(true))],
 ['duplicate local headers',d=>d.querySelector('.primary').after(d.querySelector('.primary').cloneNode(true))],
 ['duplicate header with different title',d=>{const h=d.querySelector('.primary').cloneNode(true);h.querySelector('h2').textContent='Other title';d.querySelector('.primary').after(h)}],
 ['conflicting title',d=>d.querySelector('h2').textContent='Other title'],
 ['conflicting employer',d=>d.querySelectorAll('[aria-label^="Company,"]').forEach(e=>e.setAttribute('aria-label','Company, OtherCo.'))],
 ['recommendation control only',d=>d.querySelector('.primary button').remove()],
 ['page shell is the shared owner',d=>d.querySelector('.owner').replaceWith(...d.querySelector('.owner').childNodes)],
]) test('standalone SDUI rejects '+name,()=>{
 const p=page(standalone);
 try {change(p.dom.window.document);let clicks=0;p.dom.window.document.addEventListener('click',()=>clicks++);
  const r=p.resolve(true,{title:'Senior Coordinator',company:'Acme'});
  assert.equal(r.identity_verified,false);assert.equal(r.action,'');assert.equal(clicks,0);
 }finally{p.dom.window.close()}
});
test('sticky SDUI sibling does not compete with the unique header-description block',()=>{
 const p=page('<div>'+sduiHeader('primary')+sduiDescription+'</div>'+sduiHeader('sticky'));
 try {
  let primaryClicks=0,stickyClicks=0;
  p.dom.window.document.querySelector('#primary button').onclick=()=>primaryClicks++;
  p.dom.window.document.querySelector('#sticky button').onclick=()=>stickyClicks++;
  const r=p.resolve(true);
  assert.equal(r.identity_verified,true);assert.equal(r.kind,'native');
  assert.ok(r.description.includes('Intended description'));
  assert.equal(primaryClicks,1);assert.equal(stickyClicks,0);
 }finally{p.dom.window.close()}
});
for(const [name,html] of [
 ['page-level shared description',sduiHeader('primary')+sduiDescription+sduiHeader('sticky')],
 ['two headers within one description block','<div>'+sduiHeader('primary')+sduiHeader('sticky')+sduiDescription+'</div>'],
 ['two independent description panes','<div>'+sduiHeader('primary')+sduiDescription+'</div><div>'+sduiHeader('sticky')+sduiDescription+'</div>'],
]) test('sticky disambiguation rejects '+name,()=>{
 const p=page(html);
 try{let clicks=0;p.dom.window.document.addEventListener('click',()=>clicks++);
  const r=p.resolve(true);assert.equal(r.identity_verified,false);assert.equal(clicks,0);
 }finally{p.dom.window.close()}
});
test('legacy description pane wins over condensed sibling',()=>{
 const p=page('<div data-job-id="'+target+'"><h1>Software Engineer</h1><button>Easy Apply</button><div data-job-description>Intended legacy description</div></div><div data-job-id="'+target+'"><button>Easy Apply</button></div>');
 try{const r=p.resolve(false);assert.equal(r.kind,'native');assert.ok(r.description.includes('Intended legacy description'));}
 finally{p.dom.window.close()}
});

// Execute the production SERP parser rather than a copied skip expression.
const searchScript=source.match(/const jsLinkedInSearchJobs = `([\s\S]*?)`\n/)[1];
for(const salary of ['$80K–$120K','$190K/yr - $260K/yr','A$80/hr - A$100/hr','137.8K AUD/yr – 147.4K AUD/yr','A$137,800 – A$147,400','AUD 137,800 - AUD 147,400','USD 80,000 — USD 120,000']){
 test('SERP excludes salary company-slot line: '+salary,()=>{
  const p=page('<div role="button" componentkey="job-card-component-ref-'+target+'"><p>Software Engineer</p><p>'+salary+'</p><p>11 company alumni work here</p><p>Example</p><p>Adelaide, SA</p></div>');
  try{
   // JSDOM has no layout-backed innerText.
   Object.defineProperty(p.dom.window.HTMLElement.prototype,'innerText',{get(){return this.tagName==='DIV'?[...this.children].map(x=>x.textContent).join('\n'):this.textContent;}});
   const jobs=JSON.parse(p.dom.window.eval('('+searchScript+')')());
   assert.equal(jobs.length,1);assert.equal(jobs[0].title,'Software Engineer');assert.equal(jobs[0].company,'Example');
  }finally{p.dom.window.close()}
 });
}
for(const badge of ['Actively reviewing applicants','Actively hiring','1 school alumni works here','3 school alumni work here','1 connection works here','3 connections work here']){
 test('SERP excludes badge company-slot line: '+badge,()=>{
  const p=page('<div role="button" componentkey="job-card-component-ref-'+target+'"><p>Senior Engineer</p><p>'+badge+'</p><p>Acme Corp</p><p>Sydney, NSW</p></div>');
  try{
   Object.defineProperty(p.dom.window.HTMLElement.prototype,'innerText',{get(){return this.tagName==='DIV'?[...this.children].map(x=>x.textContent).join('\n'):this.textContent;}});
   const jobs=JSON.parse(p.dom.window.eval('('+searchScript+')')());
   assert.equal(jobs.length,1);assert.equal(jobs[0].title,'Senior Engineer');assert.equal(jobs[0].company,'Acme Corp');
  }finally{p.dom.window.close()}
 });
}
test('SERP keeps plain paragraph company text beside nested spans',()=>{
 const p=page('<div role="button" componentkey="job-card-component-ref-'+target+'"><p><span>Senior Coordinator</span></p><p>Acme Corp</p><p><span>1 connection works here</span></p><p><span>Adelaide, SA (Remote)</span></p></div>');
 try {
  const jobs=JSON.parse(p.dom.window.eval('('+searchScript+')')());
  assert.equal(jobs.length,1);assert.equal(jobs[0].company,'Acme Corp');assert.equal(jobs[0].location,'Adelaide, SA (Remote)');
 } finally {p.dom.window.close()}
});
test('SERP does not turn a location into a missing company',()=>{
 const p=page('<div role="button" componentkey="job-card-component-ref-'+target+'"><span>Senior Coordinator</span><span>Adelaide, SA (Remote)</span></div>');
 try {const jobs=JSON.parse(p.dom.window.eval('('+searchScript+')')());assert.equal(jobs[0].company,'');assert.equal(jobs[0].location,'Adelaide, SA (Remote)');}
 finally {p.dom.window.close()}
});
