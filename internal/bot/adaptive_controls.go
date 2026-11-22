package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// Re-read before acting and bind one compatible current control. A reused id,
// duplicate label, removed field or conditional rerender cannot redirect a fill.
func fillAdaptiveControl(ctx context.Context, page *rod.Page, field SnapField, value string) FieldFillResult {
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
		return fillAdaptiveTypeahead(ctx, page, currentField, value)
	}
	filled, err := page.Context(ctx).Eval(jsAdaptiveFillControl, currentField, value)
	if err != nil {
		return FieldFillResult{Err: err}
	}
	if filled == nil || !filled.Value.Bool() {
		return FieldFillResult{Err: fmt.Errorf("adaptive: field %s did not retain the requested value", field.Ref)}
	}
	return FieldFillResult{Filled: true}
}

// Autocomplete accepts one exact suggestion associated with this input. Keyboard
// events are trusted browser input; a typed value alone is never success.
func fillAdaptiveTypeahead(ctx context.Context, page *rod.Page, field formField, value string) FieldFillResult {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	page = page.Context(ctx)
	el, err := page.ElementByJS(rod.Eval(jsAdaptiveTypeaheadInput, field))
	if err != nil {
		return FieldFillResult{Err: err}
	}
	if err = el.SelectAllText(); err != nil {
		return FieldFillResult{Err: err}
	}
	if err = el.Input(""); err != nil {
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
		probe, probeErr := page.Eval(jsAdaptiveTypeaheadSuggestion, field, value, false)
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
	option, err := page.ElementByJS(rod.Eval(jsAdaptiveTypeaheadSuggestion, field, value, true))
	if err != nil {
		return FieldFillResult{Err: err}
	}
	if err = option.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return FieldFillResult{Err: err}
	}
	accepted, err := page.Eval(jsAdaptiveTypeaheadAccepted, field, value)
	if err != nil {
		return FieldFillResult{Err: err}
	}
	if !accepted.Value.Bool() {
		return FieldFillResult{Err: fmt.Errorf("%w: autocomplete selection was not accepted", errAdaptiveQuestionReview)}
	}
	return FieldFillResult{Filled: true}
}

const jsAdaptiveTypeaheadHelpers = jsLinkedInApplyContainerFn + `
 const inputFor=field=>{
  const scope=resolveApplyContainer();if(!scope)return null;
  const inputs=allInDOM(scope,'input,textarea').filter(el=>!['hidden','file','radio','checkbox','submit','button','reset','image'].includes((el.type||'text').toLowerCase()));
  const candidates=field.id?inputs.filter(el=>el.id===field.id):inputs.filter((_,i)=>i===field.index);
  return candidates.length===1?candidates[0]:null;
 };
 const visible=el=>{if(!el)return false;for(let p=el;p;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.inert||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden')return false;}const r=el.getBoundingClientRect();return r.width>0&&r.height>0;};
 const listsFor=el=>{
  const ids=[...(el.getAttribute('aria-controls')||'').split(/\s+/),...(el.getAttribute('aria-owns')||'').split(/\s+/)].filter(Boolean);
  if(ids.length)return [...new Set(ids.map(id=>document.getElementById(id)).filter(visible))];
  const host=el.closest('.search-basic-typeahead,[data-test-single-typeahead-entity-form-component]');
  return host&&host.querySelectorAll('input[role=combobox]').length===1?[host]:[];
 };
 const normalize=s=>String(s||'').replace(/\s+/g,' ').trim().toLowerCase();
`
const jsAdaptiveTypeaheadInput = `(field)=>{` + jsAdaptiveTypeaheadHelpers + `return inputFor(field);}`
const jsAdaptiveTypeaheadSuggestion = `(field,value,element)=>{` + jsAdaptiveTypeaheadHelpers + `
 const input=inputFor(field);if(!input)return element?null:'missing';
 const options=[...new Set(listsFor(input).flatMap(list=>[...list.querySelectorAll('[role=option],.basic-typeahead__selectable,.search-typeahead-v2__hit')]))].filter(visible).filter(el=>el.getAttribute('aria-disabled')!=='true'&&normalize(el.textContent)===normalize(value));
 if(element)return options.length===1?options[0]:null;
 return options.length>1?'ambiguous':options.length===1?'ready':'missing';
}`
const jsAdaptiveTypeaheadAccepted = `(field,value)=>{` + jsAdaptiveTypeaheadHelpers + `
 const el=inputFor(field);return !!el&&normalize(el.value)===normalize(value)&&el.getAttribute('aria-invalid')!=='true'&&el.checkValidity()&&(el.getAttribute('aria-expanded')==='false'||!listsFor(el).some(list=>[...list.querySelectorAll('[role=option],.basic-typeahead__selectable,.search-typeahead-v2__hit')].some(visible)));
}`

const jsAdaptiveFillControl = `(field, value) => {` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();
 if(!scope)return false;
 const normalized=s=>String(s||'').trim().toLowerCase();
 const byID=(selector,id)=>allInDOM(scope,selector).filter(el=>el.id===id);
 const fire=el=>{el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}));el.dispatchEvent(new Event('blur',{bubbles:true}));};
 if(field.type==='radio'||field.type==='checkbox'){
  const options=(field.options||[]).filter(opt=>normalized(opt.label)===normalized(value)||normalized(opt.value)===normalized(value));
  if(options.length!==1)return false;
  const option=options[0];
  const choices=option.id?byID('input[type="'+field.type+'"]',option.id):allInDOM(scope,'input[type="'+field.type+'"]').filter(el=>el.name===field.name&&el.value===option.value);
  if(choices.length!==1||choices[0].disabled)return false;
  const choice=choices[0];
  let group=[choice];
  if(field.type==='checkbox'){
   group=(field.options||[]).map(opt=>{const controls=opt.id?byID('input[type=checkbox]',opt.id):allInDOM(scope,'input[type=checkbox]').filter(el=>el.name===field.name&&el.value===opt.value);return controls.length===1?controls[0]:null;});
   if(group.some(el=>!el||(el!==choice&&el.checked&&el.disabled)))return false;
   for(const other of group){if(other!==choice&&other.checked){other.click();fire(other);}}
  }
  if(!choice.checked)choice.click();
  fire(choice);
  return choice.checked&&group.every(el=>el.checkValidity()&&(field.type!=='checkbox'||el.checked===(el===choice)));
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
