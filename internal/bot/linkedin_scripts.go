package bot

// One resolver owns classification and live control lookup. It is given the intended
// job ID, never inferred from whichever recommendation is currently selected.
// No whole-document label, SDUI flag, or success-text fallback is authoritative.
const jsLinkedInResolve = `(expectedID, click = false, expandDescription = false, expectedTitle = '', expectedCompany = '', controlOnly = false) => {
 const out = {kind:'unknown', reason:'identity_unverified', expected_id:expectedID,
   observed_id:'', identity_verified:false, label:'', destination:'', url:location.origin+location.pathname,
   description:'', header_text:'', company:'', action:''};
 const finish = () => controlOnly ? null : JSON.stringify(out);
 const pathID = u => { const m = u.pathname.match(/^\/jobs\/view\/([^/]+)/); return m ? m[1] : ''; };
 const current = new URL(location.href);
 if(!/(^|\.)linkedin\.com$/i.test(current.hostname) && !['localhost','127.0.0.1','[::1]'].includes(current.hostname)) {out.reason='unexpected_origin';return finish();}
 const currentID = pathID(current);
 out.observed_id = currentID;
 if (!expectedID || currentID !== expectedID) return finish();
 const visible = el => {
   const r = el.getBoundingClientRect();
   if (r.width < 2 || r.height < 2) return false;
   for(let n=el;n; n=n.parentElement || (n.getRootNode && n.getRootNode().host)) {
     const s = getComputedStyle(n);
     if(s.display==='none' || s.visibility==='hidden' || s.visibility==='collapse' || s.opacity==='0') return false;
   }
   return true;
 };
 const all = (root, selector) => {
   const found = [...root.querySelectorAll(selector)];
   for(const el of root.querySelectorAll('*')) if(el.shadowRoot) found.push(...all(el.shadowRoot,selector));
   return found;
 };
 const parent = el => el.parentElement || (el.getRootNode && el.getRootNode().host);
 const excluded = el => {
   for(let n=el;n;n=parent(n)) if(n.matches && n.matches('aside, [class*="similar-job"], [class*="jobs-similar"], [class*="people-also-viewed"], [class*="scaffold-layout__aside"], [data-test-similar-jobs], [id^="JobDetailsSimilarJobsSlot_"], .jobs-discovery, .job-card-container, [data-occludable-job-id]')) return true;
   return false;
 };
 // Current SDUI uses paragraph titles and opaque CSS classes. Find the smallest
 // header combining a company accessible label with links to this job, plus a
 // uniquely matching job-ID-tagged description. No wrapper classes, sibling
 // positions, div counts or generic full-page text establish ownership.
 const sduiPanes = new Map();
 const jobLinkID = a => {
   try {
     const u=new URL(a.href,location.href);
     if(!['http:','https:'].includes(u.protocol)) return '';
     if(u.origin!==location.origin && !/(^|\.)linkedin\.com$/i.test(u.hostname)) return '';
     return pathID(u);
   } catch {return '';}
 };
 const descriptions=all(document,'[id^="JobDetails_AboutTheJob_"]').filter(el=>el.id==='JobDetails_AboutTheJob_'+expectedID && !excluded(el) && visible(el));
 const applyControl = el => /\beasy apply\b|\blinkedin apply\b|^apply\b/i.test(((el.getAttribute('aria-label')||'')+' '+(el.innerText||el.textContent||'')).trim());
 const submittedStatusCard=(owner,description)=>{
   const statusExcluded=el=>excluded(el)||description.contains(el)||!!el.closest('.jobs-description,.jobs-description-content__text,[data-job-description]');
   const headings=all(owner,'h2,h3,[role=heading]').filter(el=>!statusExcluded(el)&&visible(el)&&/^(your )?application status$/i.test((el.textContent||'').trim()));
   const cards=new Set();
   for(const heading of headings)for(let block=parent(heading);block&&owner.contains(block);block=parent(block)){
     if(headings.filter(el=>block.contains(el)).length!==1)break;
     const messages=all(block,'p,[role=status]').filter(el=>!statusExcluded(el)&&visible(el)&&/^application submitted$/i.test((el.textContent||'').trim()));
     if(messages.length===1){cards.add(block);break;}
   }
   return cards.size===1?[...cards][0]:null;
 };
 const hasApplyControl=all(document,'button, a[href], [role="button"]').some(el=>!excluded(el) && visible(el) && applyControl(el));
 if(descriptions.length===1) for(const link of all(document,'a[href]')) {
   if(jobLinkID(link)!==expectedID || excluded(link) || !visible(link)) continue;
   for(let header=parent(link);header && header!==document.body && header!==document.documentElement;header=parent(header)) {
     if(excluded(header)) break;
     // The primary header precedes the description. Never widen ownership to
     // the entire listing just to find an unrelated recommendation's button.
     if(header.contains(descriptions[0])) break;
     const companies=all(header,'[aria-label^="Company,"]').filter(el=>!excluded(el) && visible(el));
     if(!companies.length || !visible(header)) continue;
     const ids=all(header,'a[href]').filter(el=>!excluded(el) && visible(el)).map(jobLinkID).filter(Boolean);
     if(ids.some(id=>id!==expectedID)) break;
     if(hasApplyControl && !all(header,'button, a[href], [role="button"]').some(el=>!excluded(el) && visible(el) && applyControl(el))) continue;
     sduiPanes.set(header,{id:expectedID,description:descriptions[0]});
     break;
   }
 }
 // Standalone SDUI titles may be plain paragraphs without a job link. In that
 // layout, require unique explicit banner/description IDs and a local header
 // inside their smallest shared owner. The URL alone is never sufficient.
 if(descriptions.length===1 && sduiPanes.size===0) {
   const banners=all(document,'[id^="JobDetails_ManageJobBanner_"]').filter(el=>el.id==='JobDetails_ManageJobBanner_'+expectedID && !excluded(el));
   if(banners.length===1) {
     let owner=parent(banners[0]);
     while(owner && !owner.contains(descriptions[0])) owner=parent(owner);
     const explicitID=el=>{
       const value=el.getAttribute('data-job-id') || el.getAttribute('data-job-detail-id') || '';
       const urn=(el.getAttribute('data-entity-urn') || '').match(/^urn:li:(?:fsd_)?jobPosting:(\d+)$/);
       const marker=(el.id || '').match(/^JobDetails_[A-Za-z]+_(\d+)$/);
       return value.replace(/^urn:li:(?:fsd_)?jobPosting:/,'') || (urn&&urn[1]) || (marker&&marker[1]) || '';
     };
     const norm=s=>(s||'').toLowerCase().replace(/(?:\.{3}|…)$/,'').replace(/\s+/g,' ').trim();
     if(owner && owner!==document.body && owner!==document.documentElement && !excluded(owner) && visible(owner)) {
       const owned=[owner,...all(owner,'[id], [data-job-id], [data-job-detail-id], [data-entity-urn], a[href]')].filter(el=>!excluded(el));
       const conflict=owned.some(el=>{const id=explicitID(el);return (id && id!==expectedID) || (el.matches('a[href]') && jobLinkID(el) && jobLinkID(el)!==expectedID);});
     // Submission removes the Apply action. A read-only status can still own
     // this listing when its explicit markers, declared metadata and one local
     // status card agree. Description text and recommendations are excluded.
     let submittedCard=null;
     if(!conflict && expectedTitle && expectedCompany) {
       submittedCard=submittedStatusCard(owner,descriptions[0]);
     }
       const headers=new Map();
       if(!conflict) for(const company of all(owner,'[aria-label^="Company,"]').filter(el=>!descriptions[0].contains(el) && !excluded(el) && visible(el))) {
         for(let header=parent(company);header && header!==owner;header=parent(header)) {
           if(excluded(header) || header.contains(descriptions[0]) || header.contains(banners[0])) break;
           const actions=all(header,'button, a[href], [role="button"]').filter(el=>!excluded(el) && visible(el) && applyControl(el) && !el.querySelector('button, a[href], [role="button"]'));
           if(!visible(header)) continue;
           const titles=all(header,'h1, h2, p').filter(el=>!excluded(el) && visible(el) && (el.matches('h1,h2') || (expectedTitle && norm(el.innerText || el.textContent)===norm(expectedTitle))));
           if(!actions.length) {
             const matches=titles.filter(el=>norm(el.innerText||el.textContent)===norm(expectedTitle));
             const declaredCompany=norm((company.getAttribute('aria-label')||'').replace(/^Company,\s*/i,'').replace(/[.,]\s*$/,''));
             if(!submittedCard || matches.length!==1 || declaredCompany!==norm(expectedCompany).replace(/[.,]\s*$/,''))continue;
             headers.set(header,{title:matches[0],submittedCard});
             break;
           }
           headers.set(header,!expectedTitle || titles.length===1 ? {title:titles.length===1?titles[0]:null} : null);
           break;
         }
       }
       // Duplicate logo/company anchors share one header; independent headers
       // or a header with no verifiable title cannot borrow the same description.
       if(headers.size===1) for(const [header,entry] of headers) if(entry) sduiPanes.set(header,{id:expectedID,description:descriptions[0],title:entry.title?(entry.title.innerText || entry.title.textContent || ''):'',submittedCard:entry.submittedCard||null});
     }
   }
 }
 const ownID = el => {
   for(const attr of ['data-job-id','data-job-detail-id']) {
     const v = el.getAttribute(attr);
     if(v) return v.replace(/^urn:li:(?:fsd_)?jobPosting:/,'');
   }
   const urn=(el.getAttribute('data-entity-urn') || '').match(/^urn:li:(?:fsd_)?jobPosting:(\d+)$/);
   return urn ? urn[1] : (sduiPanes.get(el) || {}).id || '';
 };
 // A root must carry the intended ID or a job-title link identifying it. The
 // generic <main> container alone does not prove ownership of its descendants.
 const roots = [...new Set([...all(document, '.jobs-unified-top-card, .job-details-jobs-unified-top-card__container, .jobs-details__main-content, [data-job-id], [data-job-detail-id], main'), ...sduiPanes.keys()])];
 const verified = roots.filter(root => {
   if(excluded(root) || !visible(root)) return false;
   for(let n=parent(root);n;n=parent(n)) {const id=ownID(n);if(id && id!==expectedID) return false;}
   const id = ownID(root);
   if(id) return id === expectedID;
   const titleLinks = all(root, 'h1 a[href], h2 a[href], a.job-details-jobs-unified-top-card__job-title-link, .job-details-jobs-unified-top-card__job-title a[href]');
   const ids = titleLinks.filter(a=>visible(a) && !excluded(a)).map(a=>{
     try { const u=new URL(a.href,location.href); return u.origin===location.origin ? pathID(u) : ''; } catch { return ''; }
   }).filter(Boolean);
   return ids.length > 0 && ids.every(id=>id===expectedID);
 });
 // Prefer the smallest verified top-card/pane, never a broad parent containing
 // multiple independent job panes.
 const scopes = verified.filter(root=>!verified.some(other=>other!==root && root.contains(other)));
 let scope = scopes.length === 1 ? scopes[0] : null;
 if(!scope && scopes.length > 1) {
   // LinkedIn may render a sticky condensed header alongside the main pane.
   // Disambiguate by preferring the unique scope that owns the description element.
   const descPanes = scopes.filter(s => {
     const localDescriptions = all(s, '.jobs-description, .jobs-description-content__text, [data-job-description]')
       .filter(d => visible(d) && !excluded(d));
     if(localDescriptions.some(d => {
       for(let n=d;n && n!==s;n=parent(n)) {
         const id=ownID(n); if(id && id!==expectedID) return false;
       }
       return true;
     })) return true;
     const sdui=sduiPanes.get(s);
     if(!sdui) return false;
     // SDUI descriptions are siblings of their header, not children. Shared
     // page-level association alone cannot distinguish a sticky header.
     let owner=parent(s);
     while(owner && !owner.contains(sdui.description)) owner=parent(owner);
     if(!owner || owner===document.body || owner===document.documentElement) return false;
     return !scopes.some(other => other!==s && owner.contains(other));
   });
   if(descPanes.length === 1) scope = descPanes[0];
 }
 if(!scope) {out.reason='job_pane_not_unique'; return finish();}
 const root = scope;
 const headerCopy=root.cloneNode(true);
 headerCopy.querySelectorAll('aside, [class*="similar-job"], [class*="jobs-similar"], [id^="JobDetailsSimilarJobsSlot_"], .jobs-description, .jobs-description-content__text, [data-job-description], [id^="JobDetails_AboutTheJob_"]').forEach(el=>el.remove());
 headerCopy.querySelectorAll('[data-job-id], [data-job-detail-id]').forEach(el=>{const id=(el.getAttribute('data-job-id') || el.getAttribute('data-job-detail-id') || '').replace(/^urn:li:(?:fsd_)?jobPosting:/,'');if(id && id!==expectedID) el.remove();});
 out.header_text=(headerCopy.textContent || '').replace(/\s+/g,' ').trim().slice(0,4000);
 const normalized=s=>(s||'').toLowerCase().replace(/(?:\.{3}|…)$/,'').replace(/\s+/g,' ').trim();
 const companyName=s=>normalized(s).replace(/\b(pty|ltd|limited|inc|llc)\b\.?/g,'').replace(/\s+/g,' ').trim();
 const companyNames=[...new Set(all(root,'[aria-label^="Company,"], .job-details-jobs-unified-top-card__company-name, .jobs-unified-top-card__company-name, [data-testid="job-detail-company-name"]').filter(el=>visible(el)&&belongs(el)).map(el=>{
   const label=el.getAttribute('aria-label')||'';
   return (/^Company,/i.test(label)?label.replace(/^Company,\s*/i,''):el.textContent||'').replace(/\s+/g,' ').replace(/[.,]\s*$/,'').trim();
 }).filter(Boolean))];
 if(companyNames.length>1) {out.reason='company_not_unique';return finish();}
 const company=companyNames[0]||'';
 out.company=company;
 if((expectedTitle && sduiPanes.get(root)?.title && normalized(sduiPanes.get(root).title)!==normalized(expectedTitle)) ||
    (expectedTitle && (root.querySelector('h1') || sduiPanes.has(root)) && !normalized(out.header_text).includes(normalized(expectedTitle))) ||
    (expectedCompany && company && !companyName(company).includes(companyName(expectedCompany)))) {
   out.reason='listing_metadata_mismatch';return finish();
 }
 function belongs(el) {
   if(excluded(el)) return false;
   for(let n=el;n && n!==root;n=parent(n)) {
     const id=ownID(n); if(id && id!==expectedID) return false;
     if(n.matches && n.matches('section, article, li')) {
       const ids=[...n.querySelectorAll('h1 a[href], h2 a[href], h3 a[href]')].map(a=>{try{return pathID(new URL(a.href,location.href));}catch{return '';}}).filter(Boolean);
       if(ids.some(id=>id!==expectedID)) return false;
     }
   }
   return true;
 };
 out.identity_verified=true;
 out.reason='control_not_found';
 // Read only a description inside this verified pane, never the whole page.
 const descriptionSelector='.jobs-description, .jobs-description-content__text, [data-job-description]';
 const descriptionRoot=verified.find(pane=>pane.contains(root) && pane.querySelector(descriptionSelector)) || root;
 const sdui=sduiPanes.get(root);
 const desc=sdui ? sdui.description : descriptionRoot.querySelector(descriptionSelector);
 if(desc && (sdui || belongs(desc))) {
   if(expandDescription) {
     // Native SDUI descriptions can remain lazy placeholders until their
     // verified component enters the viewport. Scroll only this owned block.
     desc.scrollIntoView({block:'center',behavior:'instant'});
     const more=all(desc,'button, [role="button"]').find(el=>visible(el) && !el.disabled &&
       (el.getAttribute('data-testid')==='expandable-text-button' || /^(show|see|read) more|^…?\s*more$/i.test((el.innerText||el.textContent||'').trim())));
     if(more) {more.click();out.description_expanded=true;}
   }
   const text=desc.cloneNode(true);
   text.querySelectorAll('button, [role="button"], svg, script, style').forEach(el=>el.remove());
   text.querySelectorAll('br').forEach(el=>el.replaceWith('\n'));
   text.querySelectorAll('p, li, div, h1, h2, h3').forEach(el=>el.append('\n'));
   out.description=(text.textContent||'').replace(/[ \t]+/g,' ').replace(/\n\s*\n/g,'\n\n').trim();
 }
 // Prior application evidence must belong to the verified job pane.
 const inDescription=el=>!!(el.closest('.jobs-description, .jobs-description-content__text, [data-job-description]') || (sdui&&sdui.description&&sdui.description.contains(el)));
 let nativeSubmittedCard=sdui&&sdui.submittedCard;
 if(!nativeSubmittedCard && sdui && expectedTitle && expectedCompany && companyName(company)===companyName(expectedCompany)) {
   const titles=all(root,'h1,h2,p').filter(el=>visible(el)&&!excluded(el)&&normalized(el.innerText||el.textContent)===normalized(expectedTitle));
   let owner=parent(root);
   while(owner&&!owner.contains(sdui.description))owner=parent(owner);
   if(titles.length===1&&owner&&owner!==document.body&&owner!==document.documentElement&&!excluded(owner)&&visible(owner)) {
     const banners=all(owner,'[id^=JobDetails_ManageJobBanner_]').filter(el=>el.id==='JobDetails_ManageJobBanner_'+expectedID&&!excluded(el));
     const nodes=[owner,...all(owner,'[id],[data-job-id],[data-job-detail-id],[data-entity-urn],a[href]')].filter(el=>!excluded(el));
     const conflict=nodes.some(el=>{const marker=(el.id||'').match(/^JobDetails_[A-Za-z]+_(\d+)$/);const id=ownID(el)||(marker&&marker[1])||'';return (id&&id!==expectedID)||(el.matches('a[href]')&&jobLinkID(el)&&jobLinkID(el)!==expectedID);});
     if(banners.length===1&&!conflict) {
       nativeSubmittedCard=submittedStatusCard(owner,sdui.description);
     }
   }
 }
 if(nativeSubmittedCard&&visible(nativeSubmittedCard)){out.kind='already_applied';out.reason='verified_application_status';return finish();}
 const statusHeadings=all(root,'h2,h3,[role=heading]').filter(el=>visible(el)&&belongs(el)&&!inDescription(el)&&/^(your )?application status$/i.test((el.textContent||'').trim()));
 for(const heading of statusHeadings){
   for(let block=heading.parentElement,depth=0;block&&root.contains(block)&&depth<5;block=block.parentElement,depth++){
     if(statusHeadings.filter(el=>block.contains(el)).length!==1)break;
     const submitted=all(block,'p,[role=status]').filter(el=>visible(el)&&belongs(el)&&!inDescription(el)&&/^application submitted$/i.test((el.textContent||'').trim()));
     if(submitted.length===1){out.kind='already_applied';out.reason='verified_application_status';return finish();}
   }
 }
 const statuses=all(root,'[data-test-job-apply-button-applied], [aria-label="Applied"], [class*="application-status"], [role="status"]');
 if(statuses.some(el=>visible(el) && belongs(el) && !inDescription(el) && (el.hasAttribute('data-test-job-apply-button-applied') || el.getAttribute('aria-label')==='Applied' || /application submitted|application was sent|applied successfully/i.test(el.innerText || el.textContent || '')))) {
   out.kind='already_applied'; out.reason='verified_job_status'; return finish();
 }
 const candidates=[];
 for(const el of all(root,'button, a[href], [role="button"]')) {
   if(!visible(el) || !belongs(el) || el.disabled || el.getAttribute('aria-disabled')==='true') continue;
   // Do not accept a wrapping role=button whose descendant contains a CTA.
   if(el.querySelector('button, a[href], [role="button"]')) continue;
   const label=((el.getAttribute('aria-label') || '')+' '+(el.innerText || el.textContent || '')).toLowerCase().replace(/\s+/g,' ').trim();
   const native=/\beasy apply\b|\blinkedin apply\b/.test(label);
   const external=!native && /^(apply|apply now|apply on company website|apply on employer website)(\b|$)/.test(label);
   if(!native && !external) continue;
   let dest='';
   const href=el.getAttribute('href');
   if(href) {
     let u; try {u=new URL(href,location.href);} catch {continue;}
     if(!['http:','https:'].includes(u.protocol)) continue;
     const destID=pathID(u) || u.searchParams.get('currentJobId');
     if(destID && destID!==expectedID) continue;
     // Native links must explicitly target this job on the same origin. A
     // collection/recommendation link or a flag alone is never an apply CTA.
     if(native && (u.origin!==location.origin || pathID(u)!==expectedID)) continue;
     dest=u.origin+u.pathname; // omit tokens and tracking query parameters
   }
   candidates.push({el,label,kind:native?'native':'external',dest});
 }
 // Ambiguous native/external controls are held for manual review, never guessed.
 const kinds=new Set(candidates.map(c=>c.kind));
 if(kinds.size!==1 || candidates.length!==1) {out.reason=candidates.length?'ambiguous_controls':'control_not_found'; return finish();}
 const c=candidates[0];
 if(controlOnly) return c.kind==='native' ? c.el : null;
 out.kind=c.kind; out.reason='verified_primary_control'; out.label=c.label.slice(0,160); out.destination=c.dest;
 if(click && c.kind==='native') {
   c.el.scrollIntoView({block:'center'});
   // A single click. Do not dispatch a synthetic click followed by .click().
   c.el.click(); out.action='clicked';
 }
 return finish();
}`

// A completion dialog is evidence only after our Submit click and while the
// original job is still verified. Employer text must match the declared job
// when the confirmation names a recipient. Generic success copy is accepted only
// with verified job identity and job-scoped UI (path or data-job-id owner).
// Callers must not use this to infer a prior application before submission.
const jsLinkedInSubmissionConfirmed = `(expectedID, company, expectedTitle='') => {
 const evidence=JSON.parse((` + jsLinkedInResolve + `)(expectedID,false,false,expectedTitle,company));
 const u=new URL(location.href), pathID=(u.pathname.match(/^\/jobs\/view\/([^/]+)/)||[])[1] || '';
 if(!/(^|\.)linkedin\.com$/i.test(u.hostname) && !['localhost','127.0.0.1','[::1]'].includes(u.hostname)) return false;
 if(pathID && pathID!==expectedID) return false;
 if(evidence.identity_verified && evidence.kind==='already_applied') return true;
 const norm=s=>(s||'').toLowerCase().replace(/\s+/g,' ').trim();
 const employer=norm(company);
 const stripCorp=s=>(s||'').replace(/\b(pty|ltd|limited|inc|llc)\b\.?/g,'').replace(/\s+/g,' ').trim();
 const successRe=/application (was sent|submitted)|applied successfully|application submitted/i;
 const recipientRe=/(?:application\s+)?(?:was\s+)?(?:sent|submitted)\s+to\s+([^.,!\n]+)/i;
 const ownerJobID=el=>{
   if(!el) return '';
   const o=el.closest('[data-job-id], [data-job-detail-id]');
   if(!o) return '';
   return (o.getAttribute('data-job-id')||o.getAttribute('data-job-detail-id')||'').replace(/^urn:li:(?:fsd_)?jobPosting:/,'');
 };
 const dialogs=[...document.querySelectorAll('dialog[open], [role="dialog"], .jobs-easy-apply-modal, .jobs-easy-apply-content, [data-test-artdeco-toast-item], .artdeco-toast-item')];
 for(const el of dialogs){
   if(el.closest('[class*="msg-overlay"], aside, [class*="similar-job"]')) continue;
   const ownerID=ownerJobID(el);
   if(ownerID && ownerID!==expectedID) continue;
   const pathMatches=pathID===expectedID;
   if(!evidence.identity_verified) {
     if(ownerID!==expectedID && !pathMatches) continue;
   } else if(!pathMatches && ownerID!==expectedID) continue;
   const rect=el.getBoundingClientRect(), style=getComputedStyle(el);
   if(rect.width<2 || rect.height<2 || style.display==='none' || style.visibility==='hidden') continue;
   const text=norm(el.innerText || el.textContent);
   if(text.includes('review your application')) continue;
   if(!successRe.test(text)) continue;
   const named=recipientRe.exec(text);
   if(named && employer) {
    const got=stripCorp(norm(named[1])), want=stripCorp(employer);
    if(got && want && got!==want && !got.includes(want) && !want.includes(got)) continue;
   }
   if(employer) {
     if(text.includes(employer)) return true;
     continue;
   }
   if(!evidence.identity_verified) continue;
   const jobScoped=ownerID===expectedID || pathMatches || !!el.closest('.jobs-easy-apply-modal, .jobs-easy-apply-content');
   if(jobScoped && successRe.test(text)) return true;
 }
 return false;
}`

const jsLinkedInUncheckFollowCompany = `() => {
 const norm=s=>(s||'').toLowerCase().replace(/\s+/g,' ').trim();
 const modal=document.querySelector('.jobs-easy-apply-modal, .jobs-easy-apply-content, [data-test-modal], [role="dialog"]');
 const root=modal || document;
 const followLabel=label=>{
   const l=norm(label);
   return /follow/.test(l) && (/compan|hiring|recruiter|poster|posting|job\s+alert/i.test(l) || l.includes('follow '));
 };
 const uncheckNative=cb=>{
   if(!cb || !cb.checked) return;
   cb.checked=false;
   cb.dispatchEvent(new Event('input',{bubbles:true}));
   cb.dispatchEvent(new Event('change',{bubbles:true}));
 };
 const labelFor=el=>{
   if(el.labels && el.labels[0]) return el.labels[0].textContent || '';
   const id=el.id, lbl=id?root.querySelector('label[for="'+id+'"]'):null;
   if(lbl) return lbl.textContent || '';
   const wrap=el.closest('label');
   if(wrap) return wrap.textContent || '';
   const aria=el.getAttribute('aria-label') || '';
   if(aria) return aria;
   const near=el.closest('[data-test-form-element], fieldset, [role="group"], li');
   return near ? (near.textContent || '') : '';
 };
 for(const cb of root.querySelectorAll('input[type="checkbox"]')) {
   if(cb.id==='follow-company-checkbox' || (cb.name||'').includes('follow-company') || followLabel(labelFor(cb))) uncheckNative(cb);
 }
 for(const w of root.querySelectorAll('[role="checkbox"]')) {
   if(followLabel(w.getAttribute('aria-label') || w.textContent || '') && w.getAttribute('aria-checked')==='true') w.click();
 }
 return true;
}`

// Post-submit reconciliation only: verified intended job identity plus resolver-reported
// already-applied status. Never treat whole-page copy or unscoped controls as evidence.
const jsLinkedInAppliedLoose = `(expectedID,expectedTitle='',expectedCompany='') => {
 if(!expectedID) return false;
 const u=new URL(location.href);
 const pathID=(u.pathname.match(/^\/jobs\/view\/([^/]+)/)||[])[1]||u.searchParams.get('currentJobId')||'';
 if(!pathID || pathID!==expectedID) return false;
 const evidence=JSON.parse((` + jsLinkedInResolve + `)(expectedID,false,false,expectedTitle,expectedCompany));
 return !!(evidence.identity_verified && evidence.kind==='already_applied');
}`

// Collect job rows from the public jobs search SERP (classic list + SDUI occludable cards).
const jsLinkedInSearchJobs = `() => {
 const jobs = [];
 const seen = new Set();
 const norm = s => (s || '').trim().replace(/\s+/g, ' ');
 const inRail = el => {
   for (let n = el; n; n = n.parentElement) {
     if (n.matches && n.matches('aside, footer, [class*="similar-job"], [class*="jobs-similar"], [class*="people-also-viewed"]')) return true;
   }
   return false;
 };
 const visible = el => {
   if (!el || inRail(el)) return false;
   for (let n = el; n; n = n.parentElement) {
     const s = getComputedStyle(n);
     if (s.display === 'none' || s.visibility === 'hidden' || s.visibility === 'collapse') return false;
   }
   const r = el.getBoundingClientRect();
   if (r.width >= 2 && r.height >= 2) return true;
   // Headless or not-yet-laid-out SERP rows can report 0×0 while still being the live list.
   return el.isConnected && getComputedStyle(el).display !== 'none';
 };
 const jobIDFromHref = href => {
   if (!href) return '';
   try {
     const u = new URL(href, location.href);
     const m = u.pathname.match(/^\/jobs\/view\/([^/?#]+)/);
     return m ? m[1] : '';
   } catch { return ''; }
 };
 const add = (id, title, company, location, url, easyApply, alreadyApplied) => {
   id = String(id || '').replace(/\D/g, '');
   if (!id || seen.has(id)) return;
   seen.add(id);
   if (!url) url = 'https://www.linkedin.com/jobs/view/' + id + '/';
   jobs.push({
     id,
     title: norm(title),
     company: norm(company),
     location: norm(location),
     url,
     easyApply: !!easyApply,
     alreadyApplied: !!alreadyApplied
   });
 };
 const cardMeta = root => {
   let title = '';
   const titleEl = root.querySelector('.job-card-list__title, .job-card-container__link, a[href*="/jobs/view/"]');
   if (titleEl) title = titleEl.textContent || '';
   let company = '';
   const coEl = root.querySelector('.job-card-container__company-name, .artdeco-entity-lockup__subtitle, [data-testid="company-name"]');
   if (coEl) company = coEl.textContent || '';
   let location = '';
   const locEl = root.querySelector('.job-card-container__metadata-wrapper li span, .job-card-container__metadata-wrapper li');
   if (locEl) location = locEl.textContent || '';
   let easy = !!root.querySelector("[aria-label*='Easy Apply'], .jobs-apply-button--top-card");
   const txt = (root.innerText || '').toLowerCase();
   let applied = !!root.querySelector('.job-card-container__footer-job-state, .artdeco-inline-feedback--success')
     || txt.includes('you applied') || txt.includes('applied on');
   return { title, company, location, easy, applied };
 };
 document.querySelectorAll('.job-card-container, .jobs-search-results__list-item, li.jobs-search-results__list-item').forEach(card => {
   if (!visible(card)) return;
   let id = card.getAttribute('data-job-id') || '';
   const link = card.querySelector('a[href*="/jobs/view/"]');
   if (!id && link) id = jobIDFromHref(link.getAttribute('href'));
   const meta = cardMeta(card);
   add(id, meta.title, meta.company, meta.location, link && link.href, meta.easy, meta.applied);
 });
 document.querySelectorAll('[data-occludable-job-id]').forEach(card => {
   if (!visible(card)) return;
   const id = card.getAttribute('data-occludable-job-id') || '';
   const link = card.querySelector('a[href*="/jobs/view/"]');
   const meta = cardMeta(card);
   add(id || (link && jobIDFromHref(link.getAttribute('href'))), meta.title, meta.company, meta.location, link && link.href, meta.easy, meta.applied);
 });
 document.querySelectorAll('div[data-job-id], li[data-job-id]').forEach(card => {
   if (!visible(card) || card.matches('.job-card-container')) return;
   const id = card.getAttribute('data-job-id') || '';
   const link = card.querySelector('a[href*="/jobs/view/"]');
   const meta = cardMeta(card);
   add(id, meta.title, meta.company, meta.location, link && link.href, meta.easy, meta.applied);
 });
 // LinkedIn AI job search (/jobs/search-results): opaque classes, IDs on componentkey.
 const jobCardRefID = el => {
   const key = el.getAttribute('componentkey') || '';
   const m = key.match(/^job-card-component-ref-(\d+)$/);
   return m ? m[1] : '';
 };
 const linesFromJobCard = root => {
   if (!root) return [];
   const rendered=(root.innerText||'').split('\n').map(norm).filter(Boolean);
   if(rendered.length>1) return rendered;
   const spanLines = [];
   root.querySelectorAll('p, span').forEach(s => {
     if(s.querySelector('p, span')) return;
     const t = norm(s.textContent);
     if (t) spanLines.push(t);
   });
   if (spanLines.length > 0) return spanLines;
   return (root.innerText || '').split('\n').map(norm).filter(Boolean);
 };
 const peelSelected = l => norm(l).replace(/^selected,\s*/i, '').trim();
 const parseJobButtonLines = lines => {
   const skip = l =>
     !l ||
     /^viewed$/i.test(l) ||
     /^be an early applicant$/i.test(l) ||
     /school alumni/i.test(l) ||
     /company alumni/i.test(l) ||
     /^\d+\s+connections?\s+works?\s+here\b/i.test(l) ||
     /^actively\s+(reviewing|hiring)\b/i.test(l) ||
     /^posted /i.test(l) ||
     /^\d+ (hour|day|week|month)s? ago$/i.test(l) ||
     /\b(AUD|USD|GBP|EUR|CAD|NZD)\s*\/\s*yr\b/i.test(l) ||
     /(?:AU|US|CA|NZ|A)?[$£€]\s*\d[\d,.]*[KkMm]?\s*\/\s*(?:yr|year|mo|month|wk|week|hr|hour)\b/i.test(l) ||
     /^(?:(?:AUD|USD|GBP|EUR|CAD|NZD)\s*|(?:AU|US|CA|NZ|A)?[\$£€]\s*)?\d[\d,.]*[KkMm]?\s*[-–—]\s*(?:(?:AUD|USD|GBP|EUR|CAD|NZD)\s*|(?:AU|US|CA|NZ|A)?[\$£€]\s*)?\d[\d,.]*[KkMm]?\b/i.test(l);
   const clean = lines.map(peelSelected).filter(l => l && !skip(l));
   let title = '';
   let company = '';
   let location = '';
   for (const raw of clean) {
     const line = raw.replace(/\s*\(verified job\)\s*$/i, '').trim();
     if (!line) continue;
     if (!title) {
       title = line;
       continue;
     }
     if (line.toLowerCase() === title.toLowerCase()) continue;
     if(!location && /\((?:hybrid|on-site|remote|work from home)\)/i.test(line)) {
       location=line.replace(/\s+·\s+.*$/i,'').trim();
       continue;
     }
     if (!company) {
       company = line;
       continue;
     }
     if (
       !location &&
       (/,|\(hybrid\)|\(on-site\)|\(remote\)|\(work from home\)/i.test(line) ||
         /\b(?:NSW|VIC|QLD|SA|WA|TAS|ACT|NT)\b/.test(line))
     ) {
       location = line
         .replace(/\s+·\s+.*$/i, '')
         .replace(/\s+\d+\s+school alumni.*$/i, '')
         .replace(/\s+Posted .*$/i, '')
         .trim();
       break;
     }
   }
   return { title, company, location };
 };
 const aiRowMeta = root => {
   let easy = false;
   let applied = false;
   for (let n = root; n && n !== document.body; n = n.parentElement) {
     const t = (n.innerText || '').toLowerCase();
     if (t.length > 2500) break;
     if (/\beasy apply\b/.test(t)) easy = true;
     if (/\byou applied\b|\bapplied on\b|\bapplication submitted\b/.test(t)) applied = true;
     if (n.querySelector && n.querySelector('button[aria-label^="Dismiss "]')) break;
   }
   return { easy, applied };
 };
 document.querySelectorAll('[componentkey^="job-card-component-ref-"]').forEach(el => {
   if (!visible(el) && !el.isConnected) return;
   const id = jobCardRefID(el);
   if (!id) return;
   const btn =
     el.getAttribute('role') === 'button'
       ? el
       : el.querySelector('[role="button"][componentkey^="job-card-component-ref-"]') || el.querySelector('[role="button"]');
   const meta = parseJobButtonLines(linesFromJobCard(btn || el));
   const namedCompanies=[...new Set([...el.querySelectorAll('[aria-label^="Company,"]')].map(node=>norm(node.getAttribute('aria-label').replace(/^Company,\s*/i,'')).replace(/[.,]\s*$/,'')).filter(Boolean))];
   if(namedCompanies.length===1) meta.company=namedCompanies[0];
   const row = aiRowMeta(btn || el);
   const link = document.querySelector('a[href*="/jobs/view/' + id + '"]');
   add(id, meta.title, meta.company, meta.location, link && link.href, row.easy, row.applied);
 });
 return JSON.stringify(jobs);
}`

// Gate scraping when the SERP is a login/authwall instead of results.
const jsLinkedInSearchAuthenticated = `() => {
 const u = (location.href || '').toLowerCase();
 if (u.includes('authwall') || u.includes('/login') || u.includes('/checkpoint') || u.includes('/uas/login')) return false;
 if (document.querySelector('input[name="session_key"], form.login__form, .login__form')) return false;
 if (document.querySelector('#global-nav, .global-nav, [data-test-global-nav], .feed-identity-module')) return true;
 if (document.querySelector('[componentkey="primaryNavLinksComponentRef"], [componentkey="navBarJobTypeaheadComponentRef"]')) return true;
 if (document.querySelector('[componentkey^="job-card-component-ref-"]')) return true;
 if (/\/jobs\/search(-results)?\//i.test(location.pathname) && /\d+\s+results?/i.test(document.body.innerText || '')) return true;
 const n = JSON.parse((` + jsLinkedInSearchJobs + `)()).length;
 if (n > 0) return true;
 return false;
}`
