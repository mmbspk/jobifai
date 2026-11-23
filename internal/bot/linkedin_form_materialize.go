package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/user/jobifai/internal/domain"
)

// SDUI renders controls when its owned lazy column enters the viewport. A
// bounded scroll materializes those controls before the interpreter sees them.
const jsLinkedInMaterializeForm = `async()=>{` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();
 if(!scope||!scope.querySelector('[data-sdui-screen*=EasyApply]'))return;
 const columns=allInDOM(scope,'[data-testid=lazy-column]').filter(el=>{const r=el.getBoundingClientRect();return r.width>2&&r.height>2;});
 if(columns.length!==1)return;
 let scroller=columns[0];
 while(scroller!==scope&&scroller.scrollHeight<=scroller.clientHeight+2)scroller=scroller.parentElement;
 if(!scroller||scroller.scrollHeight<=scroller.clientHeight+2)return;
 const previous=scroller.scrollTop;let height=-1;
 for(let i=0;i<6;i++){scroller.scrollTop=scroller.scrollHeight;await new Promise(resolve=>setTimeout(resolve,80));if(height===scroller.scrollHeight)break;height=scroller.scrollHeight;}
 // Choice lists can hydrate independently of layout height. Keep the owned
 // column settled for one bounded network/UI turn before taking the snapshot.
 await new Promise(resolve=>setTimeout(resolve,900));
 scroller.scrollTop=scroller.scrollHeight;await new Promise(resolve=>setTimeout(resolve,80));
 scroller.scrollTop=previous;await new Promise(resolve=>setTimeout(resolve,80));
}`

const jsLinkedInEducationRecord = `()=>{` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();if(!scope)return '[]';
 const normalize=s=>String(s||'').replace(/\s+/g,' ').trim().toLowerCase().replace(/[ *:]+$/,'');
 const label=el=>[...scope.querySelectorAll('label[for]')].find(l=>l.htmlFor===el.id)?.textContent||el.closest('label')?.textContent||(el.getAttribute('aria-labelledby')||'').split(/\s+/).filter(Boolean).map(id=>allInDOM(scope,'[id]').find(n=>n.id===id)?.textContent||'').join(' ')||el.getAttribute('aria-label')||'';
 const shown=el=>{const r=el.getBoundingClientRect();if(r.width<2||r.height<2)return false;for(let p=el;p;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.inert||s.display==='none'||s.visibility==='hidden'||s.opacity==='0')return false;}return true;};
 const schoolCaption=s=>/^(school|school name|institution|institution name|educational institution|university|college)$/.test(normalize(s));
 const degreeCaption=s=>/^(education degree|degree|qualification|qualification type)$/.test(normalize(s));
 const studyCaption=s=>/^(education field of study|field of study|subject|major)$/.test(normalize(s));
 const controls=allInDOM(scope,'input,select,textarea').filter(shown);
 const schools=controls.filter(el=>el.matches('input,textarea')&&schoolCaption(label(el)));
 const degrees=controls.filter(el=>degreeCaption(label(el)));
 const subjects=controls.filter(el=>studyCaption(label(el)));
 const attendance=row=>allInDOM(row,'input[type=checkbox]').filter(el=>shown(el)&&/^(i )?currently attend( this institution)?$/.test(normalize(label(el)||el.parentElement?.textContent||'')));
 const rows=[];
 const years=el=>[...el.querySelectorAll('select')].filter(s=>[...s.options].filter(o=>o.value).length>0&&[...s.options].filter(o=>o.value).every(o=>/^(19|20)\d{2}$/.test(o.value)));
 const yearFor=(row,direction)=>{const found=[];for(const select of years(row)){for(let block=select.parentElement;block&&row.contains(block);block=block.parentElement){const captions=[...block.children].filter(el=>el.matches('p,label,[role=heading]')).map(el=>normalize(el.textContent));if(!captions.includes(direction))continue;if(years(block).length===1)found.push(select.value);break;}}return found.length===1?found[0]:'';};
 for(const school of schools){
  let bound=false;
  for(let row=school.parentElement;row&&row!==scope;row=row.parentElement){
   const owned=schools.filter(el=>row.contains(el)),degree=degrees.filter(el=>row.contains(el)),subject=subjects.filter(el=>row.contains(el));
   if(owned.length!==1)break;
   if(degree.length>1||subject.length>1){rows.push({ambiguous:true});bound=true;break;}
   // A declared group owns its school and unfamiliar education controls even
   // when a new caption requires model interpretation. Otherwise require the
   // native degree/subject relationship before assigning record authority.
   if(degree.length+subject.length===0&&!row.matches('fieldset,[role=group]')&&controls.length!==1)continue;
   rows.push({institution:school.value,start_year:yearFor(row,'from'),end_year:yearFor(row,'to'),currently_attending:attendance(row).some(el=>el.checked),ids:[...row.querySelectorAll('input,select,textarea')].map(el=>el.id).filter(Boolean)});bound=true;break;
  }
  if(!bound&&schools.length)rows.push({ambiguous:true});
 }
 return JSON.stringify(rows);
}`

type educationFormRow struct {
	Institution        string   `json:"institution"`
	StartYear          string   `json:"start_year"`
	EndYear            string   `json:"end_year"`
	CurrentlyAttending bool     `json:"currently_attending"`
	Ambiguous          bool     `json:"ambiguous"`
	IDs                []string `json:"ids"`
}

var educationYearPattern = regexp.MustCompile(`[12][0-9]{3}`)

func educationRowMatches(row educationFormRow, record domain.EducationDetail) bool {
	if strings.TrimSpace(row.Institution) == "" {
		return false
	}
	start, end := educationYearPattern.FindAllString(record.StartDate, -1), educationYearPattern.FindAllString(record.YearOfCompletion, -1)
	if normalizeLabel(record.Institution) == normalizeLabel(row.Institution) {
		if (row.StartYear != "" && len(start) == 1 && row.StartYear != start[0]) || (row.EndYear != "" && len(end) == 1 && row.EndYear != end[0]) {
			return false
		}
		return true
	}
	// A jobsite may append its location to the school name. The exact primary
	// institution plus both matching dates must identify the same declared row.
	primary := func(name string) string { return normalizeLabel(strings.Split(name, ",")[0]) }
	if primary(row.Institution) != primary(record.Institution) {
		return false
	}
	return len(start) == 1 && len(end) == 1 && row.StartYear == start[0] && row.EndYear == end[0]
}

func (b *Bot) prepareLinkedInScannedFields(ctx context.Context, page *rod.Page, fields []formField) error {
	profile := b.currentProfile()
	if profile == nil {
		return nil
	}
	raw, err := page.Context(ctx).Timeout(3 * time.Second).Eval(jsLinkedInEducationRecord)
	if err != nil {
		return err
	}
	var rows []educationFormRow
	if err = json.Unmarshal([]byte(raw.Value.String()), &rows); err != nil {
		return err
	}
	for _, row := range rows {
		if row.Ambiguous {
			return fmt.Errorf("%w: education row contains ambiguous degree or subject controls", errAdaptiveQuestionReview)
		}
		index := -1
		for i, record := range profile.EducationDetails {
			if educationRowMatches(row, record) {
				if index != -1 {
					return fmt.Errorf("%w: education institution matches multiple profile records", errAdaptiveQuestionReview)
				}
				index = i
			}
		}
		if index < 0 {
			return fmt.Errorf("%w: current education institution has no unique profile record", errAdaptiveQuestionReview)
		}
		if row.CurrentlyAttending && educationCompleted(profile.EducationDetails[index]) {
			// Scanning cannot change browser state. A checked stale declaration
			// must hold rather than passing through the executor's skip path.
			return fmt.Errorf("%w: current-attendance selection conflicts with completed education", errAdaptiveQuestionReview)
		}
		for i := range fields {
			for _, id := range row.IDs {
				owned := fields[i].ID == id
				for _, option := range fields[i].Options {
					if option.ID == id {
						owned = true
					}
				}
				if owned {
					fields[i].ProfileRecordPath = fmt.Sprintf("/education_details/%d", index)
				}
			}
		}
	}
	return nil
}

func educationRecord(profile *domain.ResumeProfile, path string) (domain.EducationDetail, bool) {
	if profile == nil || !strings.HasPrefix(path, "/education_details/") {
		return domain.EducationDetail{}, false
	}
	index, err := strconv.Atoi(strings.TrimPrefix(path, "/education_details/"))
	if err != nil || index < 0 || index >= len(profile.EducationDetails) {
		return domain.EducationDetail{}, false
	}
	return profile.EducationDetails[index], true
}

func resolveEducationField(field SnapField, profile *domain.ResumeProfile) (FieldResolution, bool) {
	record, ok := educationRecord(profile, field.ProfileRecordPath)
	if !ok {
		return FieldResolution{}, false
	}
	value := ""
	switch strings.Trim(countryWords(field.Label), " ") {
	case "school", "school name", "institution", "institution name", "educational institution", "university", "college":
		value = record.Institution
	case "education degree", "degree", "qualification", "qualification type":
		value = record.EducationLevel
		if field.Type == SnapTypeSelect {
			aliases := map[string]string{"msc": "MS", "m sc": "MS", "bsc": "BS", "b sc": "BS"}
			if alias := aliases[countryWords(value)]; alias != "" && !containsOptionIgnoreCase(field.Options, value) && containsOptionIgnoreCase(field.Options, alias) {
				value = alias
			}
		}
	case "education field of study", "field of study", "subject", "major":
		value = record.FieldOfStudy
	case "i currently attend this institution", "currently attend this institution", "i currently attend", "currently attend":
		if !field.Required && educationCompleted(record) {
			return FieldResolution{Ref: field.Ref, Source: SourceProfile, Action: ActionSkip}, true
		}
	default:
		return FieldResolution{}, false
	}
	if strings.TrimSpace(value) == "" {
		return FieldResolution{}, false
	}
	return makeText(field.Ref, value), true
}

func educationCompleted(record domain.EducationDetail) bool {
	if matchesAny(strings.ToLower(record.YearOfCompletion), "present", "current", "ongoing", "expected", "in progress") {
		return false
	}
	years := educationYearPattern.FindAllString(record.YearOfCompletion, -1)
	if len(years) != 1 {
		return false
	}
	year, err := strconv.Atoi(years[0])
	return err == nil && year < time.Now().Year()
}

func educationEvidenceCompatible(field SnapField, evidence []ProfileEvidence) bool {
	if field.ProfileRecordPath == "" {
		return true
	}
	for _, entry := range evidence {
		if !strings.HasPrefix(entry.Path, field.ProfileRecordPath+"/") {
			return false
		}
	}
	return len(evidence) > 0
}

func verifyEducationBinding(ctx context.Context, page *rod.Page, field SnapField, profile *domain.ResumeProfile) error {
	if field.ProfileRecordPath == "" {
		return nil
	}
	record, ok := educationRecord(profile, field.ProfileRecordPath)
	if !ok {
		return errAdaptiveQuestionReview
	}
	raw, err := page.Context(ctx).Eval(jsLinkedInEducationRecord)
	if err != nil {
		return err
	}
	var rows []educationFormRow
	if err = json.Unmarshal([]byte(raw.Value.String()), &rows); err != nil {
		return err
	}
	matches := 0
	targetID := field.ControlID
	if targetID == "" && field.BrowserField != nil && len(field.BrowserField.Options) == 1 {
		targetID = field.BrowserField.Options[0].ID
	}
	for _, row := range rows {
		if row.Ambiguous {
			return fmt.Errorf("%w: education row became ambiguous", errAdaptiveQuestionReview)
		}
		for _, id := range row.IDs {
			if id == targetID && educationRowMatches(row, record) {
				if row.CurrentlyAttending && educationCompleted(record) {
					return fmt.Errorf("%w: current-attendance selection conflicts with completed education", errAdaptiveQuestionReview)
				}
				matches++
			}
		}
	}
	if matches != 1 {
		return fmt.Errorf("%w: education record changed before the field could be verified", errAdaptiveQuestionReview)
	}
	return nil
}
