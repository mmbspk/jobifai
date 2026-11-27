(opts) => {
  const norm = s => String(s || '').replace(/\s+/g, ' ').trim();
  const lower = s => norm(s).toLowerCase();
  const wanted = String(opts.job_id || '');
  // A capture is synchronous. Reuse derived values only within this evaluation,
  // so nested SDUI wrappers do not repeat ancestor/style/text walks. Every
  // later capture still rebuilds ownership and the DOM epoch from current nodes.
  const memoize = fn => {
    const cache=new WeakMap();
    return el=>{
      if(!el) return fn(el);
      if(cache.has(el)) return cache.get(el);
      const value=fn(el);cache.set(el,value);return value;
    };
  };
  const styleFor=memoize(el=>getComputedStyle(el));
  const rawRectFor=memoize(el=>el.getBoundingClientRect());
  const descriptionSelector='[data-automation="jobAdDetails"],[data-testid="job-description"],[data-testid="jobDescriptionText"],[itemprop="description"],.jobs-description-content__text,.jobs-description__content,.jobs-box__html-content,[id^="JobDetails_AboutTheJob_"]';
  const excludedSelector = 'nav,aside,[role="navigation"],[role="complementary"],[role="dialog"],dialog,form,input,textarea,select,[data-testid*="messag"],[data-automation*="messag"],[class*="messaging"],[class*="msg-overlay"],[id*="SimilarJobs"],[id*="Recommendations"],[data-testid*="recommend"],[data-automation*="recommend"],[data-testid="competitive-jobs"],[class*="jobs-similar"],[class*="job-card"],[data-testid*="company-profile"],[data-automation*="company-profile"]';
  const idsFor = memoize(el => {
    const out = [];
    for (const key of ['data-job-id', 'data-job-detail-id']) {
      const raw = el.getAttribute && el.getAttribute(key);
      if (raw && /^\d+$/.test(raw)) out.push(raw);
    }
    const urn = (el.getAttribute && el.getAttribute('data-entity-urn') || '').match(/^urn:li:(?:fsd_)?jobPosting:(\d+)$/);
    if (urn) out.push(urn[1]);
    const id = (el.id || '').match(/^JobDetails_(?:ManageJobBanner|AboutTheJob)_(\d+)$/);
    if (id) out.push(id[1]);
    return out;
  });
  const linkedJob = memoize(link => {
    try {
      const url=new URL(link.getAttribute('href') || '',location.href),host=url.hostname.toLowerCase();
      if(url.protocol!=='https:' && url.protocol!=='http:') return null;
      if(host==='linkedin.com' || host==='www.linkedin.com') {
        const match=url.pathname.match(/^\/jobs\/view\/(\d+)(?:\/|$)/);
        const id=match ? match[1] : (url.pathname.startsWith('/jobs/search') ? url.searchParams.get('currentJobId') : '');
        return id && /^\d+$/.test(id) ? {platform:'linkedin',id,origin:'https://www.linkedin.com'} : null;
      }
      if(['au.seek.com','nz.seek.com','seek.com.au','www.seek.com.au','seek.co.nz','www.seek.co.nz'].includes(host)) {
        const match=url.pathname.match(/^\/job\/(\d+)(?:\/|$)/);
        const origin=['nz.seek.com','seek.co.nz','www.seek.co.nz'].includes(host) ? 'https://www.seek.co.nz' : 'https://www.seek.com.au';
        return match ? {platform:'seek',id:match[1],origin} : null;
      }
    } catch(e) {}
    return null;
  });
  const foreign = memoize(el => {
    for (let n = el; n && n !== document.body; n = n.parentElement) {
      const ids = idsFor(n);
      if (ids.some(id => id !== wanted)) return true;
      if(n.matches('section,article,li,[role="article"]')) {
        const links=Array.from(n.querySelectorAll('h1 a[href],h2 a[href],h3 a[href]')).filter(link=>link.closest('section,article,li,[role="article"]')===n);
        if(links.some(link=>{const job=linkedJob(link);return job && (job.id!==wanted || job.platform!==opts.platform || job.origin!==opts.origin);})) return true;
      }
    }
    return false;
  });
  const excluded = memoize(el => {
    if(!el) return false;
    if(el.closest(excludedSelector) || foreign(el)) return true;
    for(let n=el;n && n!==document.body;n=n.parentElement) if(companyHeading(n) && !knownDescription(n)) return true;
    return false;
  });
  const rectFor = memoize(el => {
    let rect = rawRectFor(el);
    if (rect.width > 0 && rect.height > 0) return { x:rect.x,y:rect.y,width:rect.width,height:rect.height };
    const rects = Array.from(el.querySelectorAll('*')).slice(0,500).filter(child => !excluded(child)).map(rawRectFor).filter(r => r.width > 0 && r.height > 0);
    if (!rects.length) return { x:rect.x,y:rect.y,width:0,height:0 };
    const left=Math.min(...rects.map(r=>r.left)),top=Math.min(...rects.map(r=>r.top));
    return {x:left,y:top,width:Math.max(...rects.map(r=>r.right))-left,height:Math.max(...rects.map(r=>r.bottom))-top};
  });
  const displayed = memoize(el => {
    for (let n=el; n && n !== document.body;n=n.parentElement) {
      const style=styleFor(n);
      if (n.hidden || n.getAttribute('aria-hidden')==='true' || style.display==='none' || style.visibility==='hidden' || style.opacity==='0') return false;
    }
    return true;
  });
  const visible = memoize(el => {
    if(!displayed(el)) return false;
    const r=rectFor(el);return r.width>0 && r.height>0;
  });
  const textFor = memoize(el => {
    const walker=document.createTreeWalker(el,NodeFilter.SHOW_TEXT);
    const parts=[];
    let node;
    while ((node=walker.nextNode())) {
      const parent=node.parentElement;
      if (!parent || excluded(parent) || !visible(parent) || parent.closest('script,style,noscript,svg,button,[role="button"]')) continue;
      const block=parent.closest('section,article');
      if (block && companyHeading(block) && !knownDescription(block)) continue;
      parts.push(node.textContent || '');
    }
    return norm(parts.join(' '));
  });
  const companyHeading = memoize(el => {
    const first=Array.from(el.children).find(n=>norm(n.textContent));
    const heading=el.matches('h1,h2,h3,h4') ? el : (first && first.matches('h1,h2,h3,h4') ? first : null);
    return !!heading && /^(about (?:the )?company|company (?:profile|overview|information)|about us|similar jobs|more jobs|recommended)/i.test(norm(heading.textContent));
  });
  const knownDescription = memoize(el => el.matches(descriptionSelector));
  const inDescription = memoize(el => !!el.closest(descriptionSelector));
  const expandLabel = el => /^(?:(?:show|see|read|view) (?:more(?: (?:of )?(?:the )?(?:job )?(?:description|details))?|(?:the )?full (?:job )?description)|(?:…|\.\.\.)?\s*more)$/i.test(norm(el.getAttribute('aria-label') || el.textContent));
  // LinkedIn renders its physical More button with aria-hidden on the button
  // itself. That exact reading control is actionable; hidden ancestors and all
  // text/header visibility checks retain the strict accessibility boundary.
  const readingControlVisible = memoize(el => {
    const physicalMore=opts.platform==='linkedin' && el.matches('button[data-testid="expandable-text-button"]');
    for(let n=el;n;n=n.parentElement) {
      const style=styleFor(n);
      if(n.hidden || n.inert || n.hasAttribute('inert') || n.getAttribute('aria-disabled')==='true' || (!(n===el && physicalMore) && n.getAttribute('aria-hidden')==='true') || style.display==='none' || style.visibility==='hidden' || style.visibility==='collapse' || style.opacity==='0') return false;
    }
    const r=rectFor(el);return r.width>0 && r.height>0;
  });
  const safeExpand = memoize(el => {
    if (!el.matches('button,[role="button"],a') || !expandLabel(el) || !readingControlVisible(el) || el.getAttribute('aria-expanded')==='true' || el.disabled) return false;
    if (el.matches('a')) {
      try {const href=new URL(el.getAttribute('href') || '#',location.href);if(href.origin!==location.origin || href.pathname!==location.pathname || href.search!==location.search) return false;}catch(e){return false;}
    }
    return !/(apply|submit|upload|resume|sign in|log in|accept|consent)/i.test(norm(el.getAttribute('aria-label') || el.textContent));
  });
  // Client-rendered SEEK pages omit JobPosting JSON-LD. Corroborate the
  // primary job reference with visible primary title/company fields. This
  // establishes reading ownership only and grants no application action.
  const seekSubject = root => {
    if(opts.platform!=='seek')return false;
    const primary=selector=>Array.from(root.querySelectorAll(selector)).filter(el=>!excluded(el)&&visible(el)&&!inDescription(el));
    const titles=new Set(primary('[data-automation="job-detail-title"],[data-testid="job-detail-title"]').map(el=>lower(el.textContent)).filter(Boolean));
    const companies=new Set(primary('[data-automation="advertiser-name"],[data-automation="job-detail-company-name"],[data-testid="job-detail-company-name"]').map(el=>lower(el.textContent)).filter(Boolean));
    if(titles.size!==1||companies.size!==1)return false;
    const refs=primary('[data-automation="job-detail-apply"],[data-testid="job-detail-apply"]').map(el=>el.closest('a[href]')).filter(el=>el&&root.contains(el));
    return refs.length>0&&refs.every(el=>{
      try{
        const href=el.getAttribute('href'),url=new URL(href,location.href),job=linkedJob(el);
        const explicitID=new URL(href,location.origin+'/').pathname.match(/^\/job\/(\d+)(?:\/|$)/);
        return explicitID&&explicitID[1]===wanted&&url.protocol==='https:'&&!url.username&&!url.password&&(!url.port||url.port==='443')&&job&&job.platform==='seek'&&job.id===wanted&&job.origin===opts.origin;
      }catch{return false;}
    });
  };
  const authRequired=Array.from(document.querySelectorAll('form[action*="login"],input[type="password"], [data-testid="login-form"]')).some(visible);
  if(authRequired) return {failure:'authentication_required',nodes:[],candidates:[],allowed_actions:[]};
  const roots = Array.from(document.querySelectorAll('main,[role="main"],.jobs-details__main-content,[data-testid="lazy-column"],[data-testid="job-details"],[data-automation="job-detail"]')).filter(el=>!excluded(el));
  const minimalRoots = roots.filter(el => !roots.some(other => other !== el && el.contains(other) && (idsFor(other).includes(wanted) || other.querySelector('[id^="JobDetails_ManageJobBanner_"],[data-job-id="'+wanted+'"]') || seekSubject(other))));
  const actualRoots = (minimalRoots.length ? minimalRoots : roots).filter(el=>visible(el) && (idsFor(el).includes(wanted) || Array.from(el.querySelectorAll('[id],[data-job-id],[data-job-detail-id],[data-entity-urn]')).some(n=>!excluded(n) && idsFor(n).includes(wanted)) || opts.trusted_verified || !!document.querySelector('script[type="application/ld+json"]') || seekSubject(el)));
  const distinctRoots=actualRoots.filter(el=>!actualRoots.some(other=>other!==el && other.contains(el)));
  if (distinctRoots.length!==1) return {failure:distinctRoots.length>1?'description_ambiguous':'identity_unverified',nodes:[],candidates:[],allowed_actions:[]};
  const scope=distinctRoots[0];
  const statusText=Array.from(scope.querySelectorAll('[data-automation*="status"],[data-testid*="status"],[role="alert"]')).filter(el=>!excluded(el)&&visible(el)).map(el=>norm(el.textContent)).join(' ');
  const expired=/(job (?:has )?expired|no longer (?:available|accepting applications)|job is closed|position (?:has been )?filled)/i.test(statusText);
  const loadingSelector='[aria-busy="true"],[role="progressbar"],[data-testid*="loading"],[data-testid*="skeleton"]';
  const explicitDescriptions=Array.from(scope.querySelectorAll(descriptionSelector)).filter(el=>!excluded(el)&&displayed(el));
  const loadingDescriptions=explicitDescriptions.map(el=>({el,text:textFor(el)}));
  const populatedDescription=loadingDescriptions.some(row=>row.text.length>=100);
  const loadingHeaders=Array.from(scope.querySelectorAll('[id^="JobDetails_ManageJobBanner_"],[data-automation="job-detail-title"],[data-testid="job-title"],[data-testid="job-detail-title"],.job-details-jobs-unified-top-card__job-title,.jobs-unified-top-card__job-title,h1,[data-automation="advertiser-name"],[data-automation="job-detail-company-name"],[data-testid="job-detail-company-name"],[data-testid="company-name"],.job-details-jobs-unified-top-card__company-name,.jobs-unified-top-card__company-name,[aria-label^="Company,"]')).filter(el=>!excluded(el)&&visible(el)&&!inDescription(el));
  const loadingTitle=lower(opts.title || opts.trusted_title);
  if(loadingTitle) loadingHeaders.push(...Array.from(scope.querySelectorAll('h2,h3,p,span')).filter(el=>!excluded(el)&&visible(el)&&!inDescription(el)&&lower(el.textContent)===loadingTitle));
  const intersects=(a,b)=>a===b || a.contains(b) || b.contains(a);
  // A populated explicit description lets unrelated widgets load independently.
  // Busy listing/header/description evidence and unknown layouts stay blocked.
  const activeLoading=[scope,...Array.from(scope.querySelectorAll(loadingSelector))].some(el=>el.matches(loadingSelector)&&!excluded(el)&&visible(el)&&(el===scope || !populatedDescription || explicitDescriptions.some(region=>intersects(el,region)) || loadingHeaders.some(region=>intersects(el,region)))) || loadingDescriptions.some(row=>row.text.length<100 && /^loading(?:\s|\.|$)/i.test(row.text));
  const loading=activeLoading || loadingDescriptions.some(row=>!row.text);
  if(opts.mode==='status') return {expired,loading,active_loading:activeLoading};
  let metadata=null;
  for (const script of document.querySelectorAll('script[type="application/ld+json"]')) {
    try {
      const parsed=JSON.parse(script.textContent);
      const records=Array.isArray(parsed)?parsed:(Array.isArray(parsed['@graph'])?parsed['@graph']:[parsed]);
      for(const record of records) {
        if(record['@type']!=='JobPosting') continue;
        const ident=record.identifier;
        const id=typeof ident==='string'?ident:String(ident && (ident.value || ident.name) || '');
        if(id===wanted) {if(metadata && (metadata.title!==record.title || metadata.hiringOrganization?.name!==record.hiringOrganization?.name)) return {failure:'identity_unverified'};metadata=record;}
      }
    } catch(e) {}
  }
  const expectedTitle=norm(opts.title || opts.trusted_title || metadata?.title);
  if(metadata && expectedTitle && lower(metadata.title)!==lower(expectedTitle)) return {failure:'identity_unverified'};
  const titleSelector='[data-automation="job-detail-title"],[data-testid="job-title"],[data-testid="job-detail-title"],.job-details-jobs-unified-top-card__job-title,.jobs-unified-top-card__job-title,h1';
  let titles=Array.from(scope.querySelectorAll(titleSelector)).filter(el=>!excluded(el) && visible(el) && !companyHeading(el) && !inDescription(el));
  if(new Set(titles.map(el=>lower(el.textContent)).filter(Boolean)).size>1) return {failure:'identity_unverified'};
  if(expectedTitle) {
    const exact=Array.from(scope.querySelectorAll('h1,h2,h3,p,span')).filter(el=>!excluded(el) && visible(el) && !inDescription(el) && lower(el.textContent)===lower(expectedTitle));
    titles=exact.filter(el=>!exact.some(child=>child!==el && el.contains(child)));
  }
  const titleValues=Array.from(new Set(titles.map(el=>norm(el.textContent)).filter(Boolean)));
  if(titleValues.length!==1 || (expectedTitle && lower(titleValues[0])!==lower(expectedTitle))) return {failure:'identity_unverified',nodes:[],candidates:[],allowed_actions:[]};
  const companySelector='[data-automation="advertiser-name"],[data-automation="job-detail-company-name"],[data-testid="job-detail-company-name"],[data-testid="company-name"],.job-details-jobs-unified-top-card__company-name,.jobs-unified-top-card__company-name,[aria-label^="Company,"]';
  const companies=Array.from(scope.querySelectorAll(companySelector)).filter(el=>!excluded(el) && visible(el) && !inDescription(el)).map(el=>({el,text:norm((el.getAttribute('aria-label')||'').replace(/^Company,\s*/i,'').replace(/\.$/,'') || el.textContent)}));
  if(new Set(companies.map(row=>lower(row.text)).filter(Boolean)).size>1) return {failure:'identity_unverified'};
  const expectedCompany=norm(opts.company || opts.trusted_company || metadata?.hiringOrganization?.name);
  if(metadata?.hiringOrganization?.name && expectedCompany && lower(metadata.hiringOrganization.name)!==lower(expectedCompany)) return {failure:'identity_unverified'};
  const relevantCompanies=companies.filter(row=>!expectedCompany || lower(row.text)===lower(expectedCompany));
  const companyValues=Array.from(new Set(relevantCompanies.map(row=>row.text).filter(Boolean)));
  if(expectedCompany && !companyValues.length && !opts.trusted_verified) return {failure:'identity_unverified',nodes:[],candidates:[],allowed_actions:[]};
  if(companyValues.length>1) return {failure:'identity_unverified',nodes:[],candidates:[],allowed_actions:[]};
  const company=companyValues[0] || norm(opts.trusted_company || metadata?.hiringOrganization?.name);
  const subjectFound=idsFor(scope).includes(wanted) || Array.from(scope.querySelectorAll('[id],[data-job-id],[data-job-detail-id],[data-entity-urn]')).some(el=>!excluded(el) && idsFor(el).includes(wanted));
  const seekReference=seekSubject(scope);
  if(!subjectFound && !metadata && !seekReference) return {failure:'identity_unverified',nodes:[],candidates:[],allowed_actions:[]};
  const headerEls=[...titles,...relevantCompanies.map(row=>row.el)];
  const elements=[scope];
  const contexts=[{el:scope,context:'listing_scope',text:''}];
  for(const el of headerEls) if(!elements.includes(el)) {elements.push(el);contexts.push({el,context:'header',text:norm(el.textContent)});}
  const possible=Array.from(scope.querySelectorAll('section,article,div,p,[itemprop="description"]')).filter(el=>!excluded(el) && displayed(el) && (visible(el) || knownDescription(el)) && !headerEls.some(h=>h===el || el.contains(h)) && (!companyHeading(el) || knownDescription(el)));
  const descriptionRoots=possible.filter(el=>knownDescription(el));
  const primaryDescriptions=descriptionRoots.filter(el=>!descriptionRoots.some(other=>other!==el && el.contains(other)));
  const candidateRoots=possible.filter(el=>!knownDescription(el) && !primaryDescriptions.some(root=>root.contains(el) || el.contains(root)) && !el.querySelector('input,textarea,select,form,nav,aside') && Array.from(el.querySelectorAll('button,[role="button"]')).every(safeExpand) && textFor(el).length>=100);
  const minimalCandidates=candidateRoots.filter(el=>!candidateRoots.some(other=>other!==el && other.contains(el)));
  for(const el of [...primaryDescriptions,...minimalCandidates]) {
    if(elements.includes(el)) continue;
    elements.push(el);contexts.push({el,context:knownDescription(el)?'description':'description_candidate',text:textFor(el)});
  }
  const readingRegions=contexts.filter(row=>row.context==='description' || row.context==='description_candidate');
  for(const row of readingRegions) for(const el of row.el.querySelectorAll('button,[role="button"],a')) {
    if(!elements.includes(el) && !excluded(el) && safeExpand(el)) {elements.push(el);contexts.push({el,context:row.context,text:norm(el.getAttribute('aria-label') || el.textContent),expand:true});}
  }
  const nodes=[],candidates=[],actions=[];
  let remaining=opts.max_text_chars, truncated=contexts.length>opts.max_nodes;
  for(let i=0;i<contexts.length && i<opts.max_nodes;i++) {
    const row=contexts[i],el=row.el,full=row.text;
    const cap=remaining,text=Array.from(full).slice(0,Math.max(0,cap)).join('');remaining-=Array.from(text).length;
    const attrs={};for(const key of ['data-job-id','data-entity-urn','data-testid','data-automation','data-test','data-sdui-screen','aria-label','itemprop','role']) {const value=el.getAttribute(key);if(value && value.length<=512) attrs[key]=value;}
    let parent=el.parentElement,parentRef='';
    while(parent && parent!==scope.parentElement) {const index=elements.indexOf(parent);if(index>=0 && index<i && index<opts.max_nodes) {parentRef='n'+index;break;}parent=parent.parentElement;}
    const hasExpand=Array.from(el.querySelectorAll('button,[role="button"],a')).some(safeExpand);
    const clipped=[el,...Array.from(el.querySelectorAll('*'))].some(n=>!excluded(n) && displayed(n) && /hidden|clip/.test(styleFor(n).overflowY) && n.scrollHeight>n.clientHeight+2);
    const collapsed=!row.expand && (el.getAttribute('aria-expanded')==='false' || el.getAttribute('data-collapsed')==='true' || hasExpand || clipped);
    const node={ref:'n'+i,parent_ref:parentRef,tag:el.tagName.toLowerCase(),role:el.getAttribute('role')||'',text,attributes:attrs,context:row.context,owner_job_id:wanted,owned:true,visible:row.expand?readingControlVisible(el):visible(el),bounds:rectFor(el),text_length:Array.from(full).length,truncated:Array.from(text).length<Array.from(full).length,collapsed,can_expand:!!row.expand,can_scroll:!row.expand && (row.context==='description'||row.context==='description_candidate')};
    truncated=truncated || node.truncated;
    nodes.push(node);
    if(!row.expand && (row.context==='description'||row.context==='description_candidate')) candidates.push(node.ref);
    if(row.expand) actions.push({kind:'expand',ref:node.ref});
    if(node.can_scroll) actions.push({kind:'scroll',ref:node.ref,amount:500});
  }
  const headerRefs=nodes.filter(n=>n.context==='header').map(n=>n.ref);
  const observedLoading=activeLoading || readingRegions.some(row=>Array.from(row.text).length<100 && /^loading(?:\s|\.|$)/i.test(row.text));
  const result={identity:{job_id:wanted,title:titleValues[0],company,verified:headerRefs.length>0,header_refs:headerRefs,evidence:[subjectFound?'subject_attribute':seekReference?'job_detail_apply_reference':'job_posting_identifier','primary_header']},nodes,candidates,allowed_actions:actions,loading:loading || observedLoading || readingRegions.some(row=>!row.text),active_loading:observedLoading,authentication_required:authRequired,expired,truncated};
  // DOM object tokens stay in the runtime and make replacing an identical
  // element invalidate a plan. They never become recipe or model attributes.
  const epochKey=Symbol.for('jobifai.jobpage.nodes');
  if(!window[epochKey]) window[epochKey]={nodes:new WeakMap(),next:1};
  const epoch=window[epochKey];
  const objectTokens=elements.map(el=>{if(!epoch.nodes.has(el))epoch.nodes.set(el,epoch.next++);return epoch.nodes.get(el);});
  result.epoch_material=JSON.stringify([result,objectTokens]);
  if(opts.expected_epoch && opts.expected_epoch!==result.epoch_material) return {failure:'stale_snapshot'};
  if(opts.mode==='action') {
    const node=nodes.find(n=>n.ref===opts.ref),index=Number(String(opts.ref || '').slice(1));
    if(!node || !actions.some(a=>a.ref===opts.ref && a.kind===opts.action)) return {failure:'invalid_plan'};
    const el=elements[index];
    if(opts.action==='scroll' && node.can_scroll && Math.abs(opts.amount)<=1000) {el.scrollIntoView({block:'center',behavior:'instant'});el.scrollBy({top:opts.amount,behavior:'instant'});}
    else return {failure:'invalid_plan'};
    return {ok:true};
  }
  if(opts.mode==='element') {
    const index=Number(String(opts.ref || '').slice(1));
    const node=nodes.find(node=>node.ref===opts.ref),el=elements[index];
    if(!node || (opts.action==='expand' && (!node.can_expand || !safeExpand(el) || !actions.some(action=>action.ref===opts.ref&&action.kind==='expand')))) return null;
    return el;
  }
  if(opts.mode==='read') {
    const selected=[];
    for(const ref of opts.refs || []) {
      const index=Number(String(ref).slice(1)),node=nodes.find(n=>n.ref===ref);
      if(!node || !candidates.includes(ref) || node.collapsed || node.truncated || !node.visible) return {failure:'description_incomplete'};
      selected.push(textFor(elements[index]));
    }
    return {description:selected.join('\n\n'),posted_date:norm(metadata?.datePosted),due_date:norm(metadata?.validThrough)};
  }
  const privacyBoxes=Array.from(document.querySelectorAll(excludedSelector)).filter(el=>visible(el)).map(rectFor);
  result.privacy_boxes=privacyBoxes;
  return result;
}
