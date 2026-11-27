package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/user/jobifai/internal/domain"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// Re-read before acting and bind one compatible current control. A reused id,
// duplicate label, removed field or conditional rerender cannot redirect a fill.
func fillAdaptiveControl(ctx context.Context, page *rod.Page, field SnapField, value string, profiles ...*domain.ResumeProfile) FieldFillResult {
	var profile *domain.ResumeProfile
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	if err := verifyEducationBinding(ctx, page, field, profile); err != nil {
		return FieldFillResult{Err: err}
	}
	raw, err := page.Context(ctx).Eval(jsScanFields, map[string]any{"includeAnsweredRadios": true, "includeAnsweredFields": true})
	if err != nil || raw == nil {
		return FieldFillResult{Err: fmt.Errorf("adaptive: current controls unreadable")}
	}
	var current []formField
	if err := json.Unmarshal([]byte(raw.Value.String()), &current); err != nil {
		return FieldFillResult{Err: err}
	}
	snap := captureStepSnapshot(current, nil, "", 0, "")
	// Vacancy scope is checked by the adapter before every side effect. It is
	// separate from the current control's observable identity.
	control := field
	control.JobContext = ""
	control.ProfileRecordPath = ""
	var matches []formField
	for i, f := range snap.Fields {
		if f.FieldSignature("") != control.FieldSignature("") {
			continue
		}
		if field.ControlID != "" && f.ControlID != field.ControlID {
			continue
		}
		matches = append(matches, current[i])
	}
	if len(matches) != 1 {
		return FieldFillResult{Err: fmt.Errorf("adaptive: stale or ambiguous current control for %s", field.Ref)}
	}
	currentField := matches[0]
	if currentField.Typeahead {
		result := fillAdaptiveTypeahead(ctx, page, currentField, value, adaptiveCityCountry(field, value, profile))
		if result.Filled {
			if err := verifyEducationBinding(ctx, page, field, profile); err != nil {
				return FieldFillResult{Err: err}
			}
		}
		return result
	}
	var selections []string
	if field.Type == SnapTypeCheckbox {
		var ok bool
		selections, ok = checkboxSelections(field, value)
		if !ok {
			return FieldFillResult{Err: fmt.Errorf("adaptive: checkbox answer has no unique current selections")}
		}
	}
	filled, err := page.Context(ctx).Eval(jsAdaptiveFillControl, currentField, value, selections)
	if err != nil {
		return FieldFillResult{Err: err}
	}
	if filled == nil || !filled.Value.Bool() {
		return FieldFillResult{Err: fmt.Errorf("adaptive: field %s did not retain the requested value", field.Ref)}
	}
	if err := verifyEducationBinding(ctx, page, field, profile); err != nil {
		return FieldFillResult{Err: err}
	}
	return FieldFillResult{Filled: true}
}

// Only an affirmative candidate-city field, filled with the current saved city,
// may use a country qualifier. Vacancy location and model text cannot supply it.
func adaptiveCityCountry(field SnapField, value string, profile *domain.ResumeProfile) string {
	if profile == nil || strings.TrimSpace(profile.PersonalInformation.City) == "" ||
		!strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(profile.PersonalInformation.City)) {
		return ""
	}
	switch countryWords(field.Label) {
	case "city", "town", "city town", "location city":
	default:
		return ""
	}
	contextText := strings.ToLower(field.Label + " " + field.Group + " " + field.HelpText)
	if personalBindingContextIsQualified(contextText) || isJobDependentQuestion(contextText) || manualDeclaration(field) {
		return ""
	}
	regionCode := profilePhoneRegion(profile.PersonalInformation.Country, nil)
	region, err := language.ParseRegion(regionCode)
	if regionCode == "" || err != nil || !region.IsCountry() {
		return ""
	}
	return display.English.Regions().Name(region)
}

// Autocomplete accepts one associated exact suggestion, or a uniquely qualified
// saved city/country tuple. Trusted input alone never proves a selection.
func fillAdaptiveTypeahead(ctx context.Context, page *rod.Page, field formField, value, country string) FieldFillResult {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	page = page.Context(ctx)
	el, err := page.ElementByJS(rod.Eval(jsAdaptiveTypeaheadInput, field))
	if err != nil {
		return FieldFillResult{Err: err}
	}
	// Rod's focus/input helpers wait for animation frames before focusing.
	// Hydrated application frames may freeze RAF; focus/select synchronously
	// and dispatch the actual replacement as bounded trusted browser input.
	if _, err = el.Eval(`()=>{this.scrollIntoView({block:'nearest',behavior:'instant'});this.focus();this.select();}`); err != nil {
		return FieldFillResult{Err: err}
	}
	if err = (proto.InputInsertText{Text: ""}).Call(page); err != nil {
		return FieldFillResult{Err: err}
	}
	for _, char := range value {
		ch := string(char)
		for _, event := range []proto.InputDispatchKeyEvent{
			{Type: proto.InputDispatchKeyEventTypeKeyDown, Key: ch},
			{Type: proto.InputDispatchKeyEventTypeChar, Key: ch, Text: ch, UnmodifiedText: ch},
			{Type: proto.InputDispatchKeyEventTypeKeyUp, Key: ch},
		} {
			if err = event.Call(page); err != nil {
				return FieldFillResult{Err: err}
			}
		}
	}
	for {
		probe, probeErr := page.Eval(jsAdaptiveTypeaheadSuggestion, field, value, false, country)
		if probeErr != nil {
			return FieldFillResult{Err: probeErr}
		}
		if probe.Value.String() == "ambiguous" {
			return FieldFillResult{Err: fmt.Errorf("%w: autocomplete suggestions are ambiguous", errAdaptiveQuestionReview)}
		}
		if probe.Value.String() == "ready" {
			break
		}
		select {
		case <-ctx.Done():
			return FieldFillResult{Err: fmt.Errorf("%w: no verified autocomplete suggestion", errAdaptiveQuestionReview)}
		case <-time.After(100 * time.Millisecond):
		}
	}
	option, err := page.ElementByJS(rod.Eval(jsAdaptiveTypeaheadSuggestion, field, value, true, country))
	if err != nil {
		return FieldFillResult{Err: err}
	}
	selected, err := option.Eval(`()=>this.textContent`)
	if err != nil {
		return FieldFillResult{Err: err}
	}
	if _, err = option.Eval(`()=>this.scrollIntoView({block:'nearest',behavior:'instant'})`); err != nil {
		return FieldFillResult{Err: err}
	}
	shape, err := option.Shape()
	if err != nil || shape.OnePointInside() == nil {
		return FieldFillResult{Err: fmt.Errorf("%w: autocomplete suggestion has no clickable point", errAdaptiveQuestionReview)}
	}
	if err = moveBoundedMouse(ctx, page, *shape.OnePointInside()); err != nil {
		return FieldFillResult{Err: err}
	}
	fresh, err := option.Eval(`(field,value,country,selected)=>this===(`+jsAdaptiveTypeaheadSuggestion+`)(field,value,true,country)&&this.textContent===selected`, field, value, country, selected.Value.String())
	if err != nil || fresh == nil || !fresh.Value.Bool() {
		return FieldFillResult{Err: fmt.Errorf("%w: autocomplete suggestion changed before selection", errAdaptiveQuestionReview)}
	}
	point, err := adaptiveTypeaheadClickPoint(ctx, page, option)
	if err != nil {
		return FieldFillResult{Err: err}
	}
	fresh, err = option.Eval(`(field,value,country,selected)=>this===(`+jsAdaptiveTypeaheadSuggestion+`)(field,value,true,country)&&this.textContent===selected`, field, value, country, selected.Value.String())
	if err != nil || fresh == nil || !fresh.Value.Bool() {
		return FieldFillResult{Err: fmt.Errorf("%w: autocomplete suggestion changed before click", errAdaptiveQuestionReview)}
	}
	if err = clickBoundedMouse(ctx, page, *point); err != nil {
		return FieldFillResult{Err: err}
	}
	accepted, err := page.Eval(jsAdaptiveTypeaheadAccepted, field, value, country, selected.Value.String())
	if err != nil {
		return FieldFillResult{Err: err}
	}
	if !accepted.Value.Bool() {
		return FieldFillResult{Err: fmt.Errorf("%w: autocomplete selection was not accepted", errAdaptiveQuestionReview)}
	}
	return FieldFillResult{Filled: true}
}

func adaptiveTypeaheadClickPoint(ctx context.Context, page *rod.Page, option *rod.Element) (*proto.Point, error) {
	if page.IsIframe() {
		// CDP's quads and node-at-point check include frame offsets. A local
		// document.elementFromPoint coordinate cannot be dispatched to the root.
		return option.Context(ctx).Interactable()
	}
	return linkedInNativeClickPoint(ctx, option)
}

const jsAdaptiveTypeaheadHelpers = jsLinkedInApplyContainerFn + `
 const inputFor=field=>{
  const scope=resolveApplyContainer();if(!scope)return null;
  const inputs=allInDOM(scope,'input,textarea').filter(el=>!['hidden','file','radio','checkbox','submit','button','reset','image'].includes((el.type||'text').toLowerCase()));
  const candidates=field.id?inputs.filter(el=>el.id===field.id):inputs.filter((_,i)=>i===field.index);
  return candidates.length===1&&visible(candidates[0])&&!candidates[0].disabled&&!candidates[0].readOnly?candidates[0]:null;
 };
 const visible=el=>{if(!el)return false;for(let p=el;p;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.inert||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden')return false;}const r=el.getBoundingClientRect();return r.width>0&&r.height>0;};
 const listsFor=el=>{
  const ids=[...(el.getAttribute('aria-controls')||'').split(/\s+/),...(el.getAttribute('aria-owns')||'').split(/\s+/)].filter(Boolean);
  if(ids.length){const lists=[...new Set(ids)].map(id=>allInDOM(document,'[id]').filter(node=>node.id===id));return lists.every(nodes=>nodes.length===1)?lists.map(nodes=>nodes[0]).filter(visible):[];}
  const host=el.closest('.search-basic-typeahead,[data-test-single-typeahead-entity-form-component]');
  return host&&host.querySelectorAll('input[role=combobox]').length===1?[host]:[];
 };
 const normalize=s=>String(s||'').replace(/\s+/g,' ').trim().toLowerCase();
 const suggestionMatches=(text,value,country)=>{
  if(normalize(text)===normalize(value))return true;
  if(!country)return false;
  const parts=String(text||'').split(',').map(normalize);
  return parts.length>=2&&parts.every(Boolean)&&parts[0]===normalize(value)&&parts[parts.length-1]===normalize(country);
 };
`
const jsAdaptiveTypeaheadInput = `(field)=>{` + jsAdaptiveTypeaheadHelpers + `return inputFor(field);}`
const jsAdaptiveTypeaheadSuggestion = `(field,value,element,country)=>{` + jsAdaptiveTypeaheadHelpers + `
 const input=inputFor(field);if(!input)return element?null:'missing';
 const options=[...new Set(listsFor(input).flatMap(list=>[...list.querySelectorAll('[role=option],.basic-typeahead__selectable,.search-typeahead-v2__hit')]))].filter(visible).filter(el=>el.getAttribute('aria-disabled')!=='true'&&suggestionMatches(el.textContent,value,country));
 if(element)return options.length===1?options[0]:null;
 return options.length>1?'ambiguous':options.length===1?'ready':'missing';
}`
const jsAdaptiveTypeaheadAccepted = `(field,value,country,selected)=>{` + jsAdaptiveTypeaheadHelpers + `
 const el=inputFor(field);return !!el&&suggestionMatches(el.value,value,country)&&normalize(el.value)===normalize(selected||value)&&el.getAttribute('aria-invalid')!=='true'&&el.checkValidity()&&(el.getAttribute('aria-expanded')==='false'||!listsFor(el).some(list=>[...list.querySelectorAll('[role=option],.basic-typeahead__selectable,.search-typeahead-v2__hit')].some(visible)));
}`

const jsAdaptiveFillControl = `(field, value, checkboxSelection) => {` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();
 if(!scope)return false;
 const normalized=s=>String(s||'').trim().toLowerCase();
 const byID=(selector,id)=>allInDOM(scope,selector).filter(el=>el.id===id);
 const fire=el=>{el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}));el.dispatchEvent(new Event('blur',{bubbles:true}));};
 if(field.type==='checkbox'){
  const options=field.options||[];
  const single=options.filter(opt=>normalized(opt.label)===normalized(value)||normalized(opt.value)===normalized(value));
  const labels=Array.isArray(checkboxSelection)?checkboxSelection:(single.length===1?[single[0].label]:[]);
  if(!labels.length||new Set(labels.map(normalized)).size!==labels.length)return false;
  const selected=new Set();
  for(const label of labels){const matches=options.map((opt,index)=>normalized(opt.label)===normalized(label)?index:-1).filter(index=>index>=0);if(matches.length!==1)return false;selected.add(matches[0]);}
  const resolveGroup=()=>{
   if(resolveApplyContainer()!==scope)return null;
   const group=options.map(opt=>{
    const controls=opt.id?byID('input[type=checkbox]',opt.id):allInDOM(scope,'input[type=checkbox]').filter(el=>el.name===field.name&&el.value===opt.value);
    if(controls.length!==1)return null;
    const control=controls[0];
    if((control.value||control.id)!==opt.value||(control.name&&control.name!==field.name))return null;
    const associated=control.id?allInDOM(scope,'label[for]').filter(el=>el.htmlFor===control.id):[];
    if(associated.length>1)return null;
    const caption=associated[0]||control.closest('label')||control.parentElement;
    if(!caption||normalized(caption.textContent.replace(/\s+/g,' '))!==normalized(opt.label))return null;
    return control;
   });
   if(group.some((control,index)=>!control||(control.disabled&&(selected.has(index)||control.checked))))return null;
   return group;
  };
  if(!resolveGroup())return false;
  for(let index=0;index<options.length;index++){
   const group=resolveGroup();if(!group)return false;
   const control=group[index];
   if(control.checked!==selected.has(index)){control.click();fire(control);}
  }
  const group=resolveGroup();
  return !!group&&group.every((control,index)=>control.checked===selected.has(index)&&control.checkValidity());
 }
 if(field.type==='radio'){
  const options=(field.options||[]).filter(opt=>normalized(opt.label)===normalized(value)||normalized(opt.value)===normalized(value));
  if(options.length!==1)return false;
  const option=options[0];
  const choices=option.id?byID('input[type="'+field.type+'"]',option.id):allInDOM(scope,'input[type="'+field.type+'"]').filter(el=>el.name===field.name&&el.value===option.value);
  if(choices.length!==1||choices[0].disabled)return false;
  const choice=choices[0];
  if(!choice.checked)choice.click();
  fire(choice);
  return choice.checked&&choice.checkValidity();
 }
 let candidates=field.id?byID('input,select,textarea',field.id):[];
 if(!field.id&&field.type==='text'){
  const inputs=allInDOM(scope,'input,textarea').filter(el=>{
   const type=(el.type||'text').toLowerCase();
   if(['hidden','file','radio','checkbox','submit','button','reset','image'].includes(type))return false;
   const r=el.getBoundingClientRect();return r.width>0&&r.height>0;
  });
  if(inputs[field.index])candidates=[inputs[field.index]];
 }
 if(candidates.length!==1)return false;
 const el=candidates[0];
 if(el.disabled||el.readOnly)return false;
 const view=el.ownerDocument.defaultView;
 let next=String(value);
 let proto=el.tagName==='SELECT'?view.HTMLSelectElement.prototype:el.tagName==='TEXTAREA'?view.HTMLTextAreaElement.prototype:view.HTMLInputElement.prototype;
 if(el.tagName==='SELECT'){
  const options=[...el.options].filter(opt=>!opt.disabled&&(normalized(opt.text)===normalized(value)||normalized(opt.value)===normalized(value)));
  if(options.length!==1)return false;
  next=options[0].value;
 }
 const setter=Object.getOwnPropertyDescriptor(proto,'value').set;
 setter.call(el,next);fire(el);
 return el.value===next && el.checkValidity();
}`
