package bot

// One resolver owns both classification and clicking. It is given the intended
// job ID, never inferred from whichever recommendation is currently selected.
// No whole-document label, SDUI flag, or success-text fallback is authoritative.
const jsLinkedInResolve = `(expectedID, click = false, expandDescription = false, expectedTitle = '', expectedCompany = '') => {
 const out = {kind:'unknown', reason:'identity_unverified', expected_id:expectedID,
   observed_id:'', identity_verified:false, label:'', destination:'', url:location.origin+location.pathname,
   description:'', header_text:'', action:''};
 const finish = () => JSON.stringify(out);
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
 if(scopes.length !== 1) {out.reason='job_pane_not_unique'; return finish();}
 const root = scopes[0];
 const headerCopy=root.cloneNode(true);
 headerCopy.querySelectorAll('aside, [class*="similar-job"], [class*="jobs-similar"], [id^="JobDetailsSimilarJobsSlot_"], .jobs-description, .jobs-description-content__text, [data-job-description], [id^="JobDetails_AboutTheJob_"]').forEach(el=>el.remove());
 headerCopy.querySelectorAll('[data-job-id], [data-job-detail-id]').forEach(el=>{const id=(el.getAttribute('data-job-id') || el.getAttribute('data-job-detail-id') || '').replace(/^urn:li:(?:fsd_)?jobPosting:/,'');if(id && id!==expectedID) el.remove();});
 out.header_text=(headerCopy.textContent || '').replace(/\s+/g,' ').trim().slice(0,4000);
 const normalized=s=>(s||'').toLowerCase().replace(/(?:\.{3}|…)$/,'').replace(/\s+/g,' ').trim();
 const companyName=s=>normalized(s).replace(/\b(pty|ltd|limited|inc|llc)\b\.?/g,'').replace(/\s+/g,' ').trim();
 const companyEl=root.querySelector('[aria-label^="Company,"]');
 const company=companyEl ? companyEl.getAttribute('aria-label').replace(/^Company,\s*/i,'').replace(/[.,]\s*$/,'') : '';
 if((expectedTitle && (root.querySelector('h1') || sduiPanes.has(root)) && !normalized(out.header_text).includes(normalized(expectedTitle))) ||
    (expectedCompany && company && !companyName(company).includes(companyName(expectedCompany)))) {
   out.reason='listing_metadata_mismatch';return finish();
 }
 const belongs = el => {
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
     const more=all(desc,'button, [role="button"]').find(el=>visible(el) && !el.disabled &&
       (el.getAttribute('data-testid')==='expandable-text-button' || /^(show|see|read) more|^…?\s*more$/i.test((el.innerText||el.textContent||'').trim())));
     if(more) more.click();
   }
   const text=desc.cloneNode(true);
   text.querySelectorAll('button, [role="button"], svg, script, style').forEach(el=>el.remove());
   text.querySelectorAll('br').forEach(el=>el.replaceWith('\n'));
   text.querySelectorAll('p, li, div, h1, h2, h3').forEach(el=>el.append('\n'));
   out.description=(text.textContent||'').replace(/[ \t]+/g,' ').replace(/\n\s*\n/g,'\n\n').trim();
 }
 // Prior application evidence must belong to the verified job pane.
 const statuses=all(root,'[data-test-job-apply-button-applied], [aria-label="Applied"], [class*="application-status"], [role="status"]');
 if(statuses.some(el=>visible(el) && belongs(el) && (el.hasAttribute('data-test-job-apply-button-applied') || el.getAttribute('aria-label')==='Applied' || /application submitted|application was sent|applied successfully/i.test(el.innerText || el.textContent || '')))) {
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
// when the confirmation names a recipient; Applied CTA and generic success copy
// are accepted when the job path still matches.
// Callers must not use this to infer a prior application before submission.
const jsLinkedInSubmissionConfirmed = `(expectedID, company) => {
 const evidence=JSON.parse((` + jsLinkedInResolve + `)(expectedID,false));
 const u=new URL(location.href), pathID=(u.pathname.match(/^\/jobs\/view\/([^/]+)/)||[])[1] || '';
 if(!/(^|\.)linkedin\.com$/i.test(u.hostname) && !['localhost','127.0.0.1','[::1]'].includes(u.hostname)) return false;
 if(pathID && pathID!==expectedID) return false;
 if(evidence.identity_verified && evidence.kind==='already_applied') return true;
 const norm=s=>(s||'').toLowerCase().replace(/\s+/g,' ').trim();
 const employer=norm(company);
 const successRe=/application (was sent|submitted)|applied successfully|application submitted/i;
 const dialogs=[...document.querySelectorAll('[role="dialog"], .jobs-easy-apply-modal, .jobs-easy-apply-content, [data-test-artdeco-toast-item], .artdeco-toast-item')];
 for(const el of dialogs){
   if(el.closest('[class*="msg-overlay"], aside, [class*="similar-job"]')) continue;
   const owner=el.closest('[data-job-id], [data-job-detail-id]');
   if(owner && (owner.getAttribute('data-job-id') || owner.getAttribute('data-job-detail-id'))!==expectedID) continue;
   if(pathID!==expectedID && (!owner || (owner.getAttribute('data-job-id') || owner.getAttribute('data-job-detail-id'))!==expectedID)) continue;
   const rect=el.getBoundingClientRect(), style=getComputedStyle(el);
   if(rect.width<2 || rect.height<2 || style.display==='none' || style.visibility==='hidden') continue;
   const text=norm(el.innerText || el.textContent);
   if(text.includes('review your application')) continue;
   if(!successRe.test(text)) continue;
   if(!employer || text.includes(employer)) return true;
   if(text.includes('application submitted') || text.includes('applied successfully')) return true;
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
const jsLinkedInAppliedLoose = `(expectedID) => {
 if(!expectedID) return false;
 const u=new URL(location.href);
 const pathID=(u.pathname.match(/^\/jobs\/view\/([^/]+)/)||[])[1]||u.searchParams.get('currentJobId')||'';
 if(!pathID || pathID!==expectedID) return false;
 const evidence=JSON.parse((` + jsLinkedInResolve + `)(expectedID,false));
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
   const spanLines = [];
   root.querySelectorAll('span').forEach(s => {
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
     /school alumni$/i.test(l) ||
     /^posted /i.test(l) ||
     /^\d+ (hour|day|week|month)s? ago$/i.test(l);
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
