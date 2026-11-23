package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

func seekLibraryFixtureRows() []seekResumeLibraryRow {
	return []seekResumeLibraryRow{
		{ID: "newest", Label: "23/11/26 - default.pdf", Default: true},
		{ID: "selected", Label: "7/9/26 - current.pdf", Selected: true},
		{ID: "other", Label: "20/4/26 - other.pdf"},
		{ID: "oldest", Label: "18/4/26 - old.pdf"},
	}
}

func TestSeekResumeLibraryRecovery_InteractiveModalOverridesPortalPointerEvents(t *testing.T) {
	page := rodTestPage(t, seekLibraryFixtureHTML(seekLibraryFixtureRows(), "normal", true))
	_, err := page.Eval(`()=>{const dialog=document.getElementById('resume-limit-exceeded-warning'),portal=document.createElement('div');portal.style.pointerEvents='none';dialog.before(portal);portal.append(dialog);dialog.style.pointerEvents='auto';}`)
	require.NoError(t, err)
	b := newTestBot(t)
	recovered, err := b.seekRecoverResumeLibrary(page, "approved.pdf")
	require.NoError(t, err)
	require.True(t, recovered)
	value, err := page.Eval(`()=>window.deleted`)
	require.NoError(t, err)
	require.Len(t, value.Value.Arr(), 1)
	require.Equal(t, "oldest", value.Value.Get("0.id").String())
	require.True(t, value.Value.Get("0.trusted").Bool())
}

func TestSeekResumeLibraryRecovery_SelectionCannotChangeDuringVisibilityWait(t *testing.T) {
	page := rodTestPage(t, seekLibraryFixtureHTML(seekLibraryFixtureRows(), "exact", true))
	_, err := page.Eval(`()=>{const button=document.getElementById('delete');button.onmouseover=()=>{button.style.opacity='0';setTimeout(()=>{const select=document.getElementById('docLimitExceededDropdown');select.value='other';select.dispatchEvent(new Event('change',{bubbles:true}));button.style.opacity='1'},100);};}`)
	require.NoError(t, err)
	recovered, err := newTestBot(t).seekRecoverResumeLibrary(page, "approved.pdf")
	require.Error(t, err)
	require.False(t, recovered)
	value, err := page.Eval(`()=>[window.deleted.length,document.querySelectorAll('[data-library-id]').length]`)
	require.NoError(t, err)
	require.Zero(t, value.Value.Get("0").Int())
	require.Equal(t, 4, value.Value.Get("1").Int())
}

func TestSeekResumeEviction_RanksDatesRatherThanDOMOrFilename(t *testing.T) {
	for _, reversed := range []bool{false, true} {
		rows := seekLibraryFixtureRows()
		if reversed {
			for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
		target, source, err := chooseSeekResumeEviction(rows, "approved.pdf", nil, nil)
		require.NoError(t, err)
		require.Equal(t, "oldest", target.ID)
		require.Equal(t, "saved_date_fallback", source)
	}
}

func TestSeekResumeEviction_UsesLastUseAndProtectsApprovedSelectedDefault(t *testing.T) {
	rows := seekLibraryFixtureRows()
	rows[3].LastUsed = "2026-11-22T00:00:00Z"
	target, source, err := chooseSeekResumeEviction(rows, "approved.pdf", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "other", target.ID)
	require.Equal(t, "saved_date_fallback", source)
	rows[3].LastUsed = ""
	uses := map[string]seekResumeUse{"oldest": {Filename: "old.pdf", UsedAt: time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)}}
	target, _, err = chooseSeekResumeEviction(rows, "approved.pdf", uses, nil)
	require.NoError(t, err)
	require.Equal(t, "other", target.ID)
	target, _, err = chooseSeekResumeEviction(rows, "other.pdf", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "oldest", target.ID)
	rows[3].Selected = true
	_, _, err = chooseSeekResumeEviction(rows, "other.pdf", nil, nil)
	require.Error(t, err)
}

func TestSeekResumeEviction_DuplicateFilenamesKeepDocumentIdentity(t *testing.T) {
	rows := []seekResumeLibraryRow{{ID: "a", Label: "18/4/26 - Resume.pdf"}, {ID: "b", Label: "20/4/26 - Resume.pdf"}}
	uses := map[string]seekResumeUse{"a": {Filename: "resume.pdf", UsedAt: time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)}}
	target, _, err := chooseSeekResumeEviction(rows, "approved.pdf", uses, nil)
	require.NoError(t, err)
	require.Equal(t, "b", target.ID)
	// A filename-only historical record cannot distinguish either duplicate.
	recent := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	rows = append(rows, seekResumeLibraryRow{ID: "c", Label: "7/9/26 - third.pdf"})
	target, _, err = chooseSeekResumeEviction(rows, "approved.pdf", uses, map[string]time.Time{"resume.pdf": recent})
	require.NoError(t, err)
	require.Equal(t, "c", target.ID)
}

func TestSeekResumeEviction_UnknownOrConflictingMetadataHolds(t *testing.T) {
	for _, row := range []seekResumeLibraryRow{{ID: "a", Label: "Resume.pdf"}, {ID: "a", Label: "31/2/26 - Resume.pdf"}, {ID: "a", Label: "18/4/26 - Resume.pdf", LastUsed: "yesterday"}} {
		_, _, err := chooseSeekResumeEviction([]seekResumeLibraryRow{row}, "approved.pdf", nil, nil)
		require.Error(t, err)
	}
}

func seekLibraryFixtureHTML(rows []seekResumeLibraryRow, behavior string, showDialog bool) string {
	var saved, options strings.Builder
	for _, row := range rows {
		split := strings.SplitN(row.Label, " - ", 2)
		name := split[len(split)-1]
		checked, badge := "", ""
		if row.Selected {
			checked = " checked"
		}
		if row.Default {
			badge = "<span>Default</span>"
		}
		fmt.Fprintf(&saved, `<div data-library-id="%s"><input type=radio name=document-select-library id="radio-%s" value="%s"%s><label for="radio-%s">%s</label>%s</div>`, html.EscapeString(row.ID), html.EscapeString(row.ID), html.EscapeString(row.ID), checked, html.EscapeString(row.ID), html.EscapeString(name), badge)
		fmt.Fprintf(&options, `<option value="%s" data-last-used-at="%s">%s</option>`, html.EscapeString(row.ID), html.EscapeString(row.LastUsed), html.EscapeString(row.Label))
	}
	hidden := " hidden"
	if showDialog {
		hidden = ""
	}
	behaviorJSON, _ := json.Marshal(behavior)
	return fmt.Sprintf(`<html><body><main data-testid="quick-apply-form"><h1>Applying for Coordinator</h1><div id=library>%s</div><div data-testid=resumeFileInput><h3>Resumé</h3><button id=upload>Upload</button><input id=resume type=file hidden></div><div data-testid=coverLetterFileInput><input id=cover type=file hidden></div><input type=checkbox id=default-toggle><button>Continue</button></main><div role=dialog id=resume-limit-exceeded-warning style="position:fixed;top:20px;left:420px;background:white;padding:20px;z-index:10"%s><h3>Resumé limit reached</h3><p data-testid=10-resume-limit-text>Please select a resumé to delete from your list and try again.</p><select id=docLimitExceededDropdown><option value="" disabled selected>Please select a resumé</option>%s</select><button data-automation=10-resume-delete id=delete disabled>Delete</button></div><script>
 const behavior=%s;window.deleted=[];window.uploadClicks=0;
 const warning=document.getElementById('resume-limit-exceeded-warning'),select=document.getElementById('docLimitExceededDropdown'),deleteButton=document.getElementById('delete'),library=document.getElementById('library');
 select.onchange=()=>{deleteButton.disabled=!select.value;if(behavior==='changed-choice')select.options[1].textContent='23/11/26 - changed.pdf';};
 deleteButton.onclick=e=>{window.deleted.push({id:select.value,trusted:e.isTrusted});if(behavior==='dialog-only'){warning.hidden=true;return;}const id=behavior==='wrong-row'?'other':select.value;document.querySelector('[data-library-id="'+id+'"]').remove();if(behavior==='extra-row')document.querySelector('[data-library-id=other]').remove();select.querySelector('option[value="'+id+'"]').remove();warning.hidden=true;};
 document.getElementById('upload').onclick=()=>{window.uploadClicks++;if(library.children.length>=%d){warning.hidden=false;return;}document.getElementById('resume').click();};
 document.getElementById('resume').onchange=()=>{const file=document.getElementById('resume').files[0];if(!file)return;library.querySelectorAll('input[type=radio]').forEach(r=>r.checked=false);const row=document.createElement('div');row.dataset.libraryId='uploaded';const radio=document.createElement('input');radio.type='radio';radio.name='document-select-library';radio.id='radio-uploaded';radio.value='uploaded';radio.checked=true;const label=document.createElement('label');label.htmlFor=radio.id;label.textContent=file.name;row.append(radio,label);library.append(row);const option=document.createElement('option');option.value='uploaded';option.textContent='23/11/26 - '+file.name;select.append(option);};
 </script></body></html>`, saved.String(), hidden, options.String(), behaviorJSON, len(rows))
}

func TestSeekResumeLibraryRecovery_DeletesExactlyRankedID(t *testing.T) {
	for _, reversed := range []bool{false, true} {
		rows := seekLibraryFixtureRows()
		if reversed {
			for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
		page := rodTestPage(t, seekLibraryFixtureHTML(rows, "exact", true))
		recovered, err := New(Config{Platform: domain.PlatformSeek}).seekRecoverResumeLibrary(page, "approved.pdf")
		require.NoError(t, err)
		require.True(t, recovered)
		state, err := page.Eval(`()=>({deleted:window.deleted,remaining:[...document.querySelectorAll('#library input')].map(r=>r.value),defaultToggle:document.getElementById('default-toggle').checked})`)
		require.NoError(t, err)
		require.Len(t, state.Value.Get("deleted").Arr(), 1)
		require.Equal(t, "oldest", state.Value.Get("deleted.0.id").String())
		require.True(t, state.Value.Get("deleted.0.trusted").Bool())
		require.ElementsMatch(t, []string{"newest", "selected", "other"}, []string{state.Value.Get("remaining.0").String(), state.Value.Get("remaining.1").String(), state.Value.Get("remaining.2").String()})
		require.False(t, state.Value.Get("defaultToggle").Bool())
	}
}

func TestSeekResumeLibraryRecovery_AmbiguousOrChangedLibraryHasNoDelete(t *testing.T) {
	for _, mode := range []string{"duplicate button", "duplicate identity", "changed-choice", "mismatched filename"} {
		t.Run(mode, func(t *testing.T) {
			page := rodTestPage(t, seekLibraryFixtureHTML(seekLibraryFixtureRows(), mode, true))
			mutation := map[string]string{"duplicate button": `()=>document.getElementById('delete').after(document.getElementById('delete').cloneNode(true))`, "duplicate identity": `()=>document.getElementById('docLimitExceededDropdown').append(document.getElementById('docLimitExceededDropdown').options[1].cloneNode(true))`, "mismatched filename": `()=>document.querySelector('label[for=radio-oldest]').textContent='another.pdf'`}[mode]
			if mutation != "" {
				_, err := page.Eval(mutation)
				require.NoError(t, err)
			}
			recovered, err := New(Config{Platform: domain.PlatformSeek}).seekRecoverResumeLibrary(page, "approved.pdf")
			require.ErrorIs(t, err, errAdaptiveDocumentReview)
			require.False(t, recovered)
			state, err := page.Eval(`()=>window.deleted.length`)
			require.NoError(t, err)
			require.Zero(t, state.Value.Int())
		})
	}
}

func TestSeekResumeLibraryRecovery_RequiresExactRemovalAcknowledgement(t *testing.T) {
	for _, mode := range []string{"dialog-only", "wrong-row", "extra-row"} {
		t.Run(mode, func(t *testing.T) {
			page := rodTestPage(t, seekLibraryFixtureHTML(seekLibraryFixtureRows(), mode, true))
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			defer cancel()
			recovered, err := New(Config{Platform: domain.PlatformSeek}).seekRecoverResumeLibrary(page.Context(ctx), "approved.pdf")
			require.Error(t, err)
			require.False(t, recovered)
			state, err := page.Eval(`()=>[window.deleted.length,window.uploadClicks]`)
			require.NoError(t, err)
			require.Equal(t, 1, state.Value.Get("0").Int())
			require.Zero(t, state.Value.Get("1").Int())
		})
	}
}

func TestSeekResumeLibraryRecovery_UploadRetriesAfterOneVerifiedEviction(t *testing.T) {
	page := rodTestPage(t, seekLibraryFixtureHTML(seekLibraryFixtureRows(), "exact", false))
	dir := t.TempDir()
	path := filepath.Join(dir, "approved-resume.pdf")
	require.NoError(t, os.WriteFile(path, []byte("fixture approved PDF"), 0600))
	b := New(Config{Platform: domain.PlatformSeek})
	require.NoError(t, b.seekUploadResume(page, path))
	state, err := page.Eval(`()=>({deleted:window.deleted,uploads:window.uploadClicks,resume:document.getElementById('resume').files[0].name,cover:document.getElementById('cover').files.length,selected:document.getElementById('radio-uploaded').checked,defaultToggle:document.getElementById('default-toggle').checked})`)
	require.NoError(t, err)
	require.Len(t, state.Value.Get("deleted").Arr(), 1)
	require.Equal(t, "oldest", state.Value.Get("deleted.0.id").String())
	require.Equal(t, 2, state.Value.Get("uploads").Int())
	require.Equal(t, "approved-resume.pdf", state.Value.Get("resume").String())
	require.Zero(t, state.Value.Get("cover").Int())
	require.True(t, state.Value.Get("selected").Bool())
	require.False(t, state.Value.Get("defaultToggle").Bool())
	// A repeated request reuses the exact selected approved attachment.
	require.NoError(t, b.seekUploadResume(page, path))
	state, err = page.Eval(`()=>[window.deleted.length,window.uploadClicks]`)
	require.NoError(t, err)
	require.Equal(t, 1, state.Value.Get("0").Int())
	require.Equal(t, 2, state.Value.Get("1").Int())
}

func TestSeekResumeLibraryRecovery_UsageIsScopedByOwnerAndDocumentID(t *testing.T) {
	page := rodTestPage(t, seekLibraryFixtureHTML(seekLibraryFixtureRows(), "exact", true))
	db := openMappingDB(t)
	b := New(Config{Platform: domain.PlatformSeek, DB: db, UserID: "candidate"})
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES('candidate','candidate@fixture.test','fixture'),('other-owner','other@fixture.test','fixture')`)
	require.NoError(t, err)
	b.seekRecordResumeUse(page, "current.pdf")
	uses, _, err := b.seekResumeUses(context.Background())
	require.NoError(t, err)
	require.Contains(t, uses, "selected")
	require.Equal(t, "current.pdf", uses["selected"].Filename)
	other := New(Config{Platform: domain.PlatformSeek, DB: db, UserID: "other-owner"})
	uses, _, err = other.seekResumeUses(context.Background())
	require.NoError(t, err)
	require.Empty(t, uses)
	// Underscores in the namespace must remain literal, rather than acting
	// as SQL LIKE wildcards and reading an unrelated setting.
	_, err = db.Exec(`INSERT INTO settings(user_id,key,value) VALUES('candidate','seekXresumeXlastXuse:unrelated','not valid json')`)
	require.NoError(t, err)
	uses, _, err = b.seekResumeUses(context.Background())
	require.NoError(t, err)
	require.Len(t, uses, 1)
}

func TestSeekResumeLibraryRecovery_AdaptiveRollbackPreventsDeletion(t *testing.T) {
	page := rodTestPage(t, seekLibraryFixtureHTML(seekLibraryFixtureRows(), "exact", true))
	store := config.NewStore(openMappingDB(t))
	require.NoError(t, store.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineLegacy}))
	b := New(Config{Platform: domain.PlatformSeek, UserID: "candidate", EngineStore: store, EngineMode: domain.ApplyEngineAdaptive})
	recovered, err := b.seekRecoverResumeLibrary(page, "approved.pdf")
	require.ErrorIs(t, err, errEngineModeReverted)
	require.False(t, recovered)
	state, err := page.Eval(`()=>[window.deleted.length,document.getElementById('docLimitExceededDropdown').value]`)
	require.NoError(t, err)
	require.Zero(t, state.Value.Get("0").Int())
	require.Empty(t, state.Value.Get("1").String())
	// The zero-value production mode continues to behave as Legacy.
	b.cfg.EngineMode = ""
	require.NoError(t, b.seekResumeLibraryGuard())
}

func TestSeekResumeLibraryRecovery_HiddenNativeRadiosAndUploadMethods(t *testing.T) {
	page := rodTestPage(t, seekLibraryFixtureHTML(seekLibraryFixtureRows(), "exact", true))
	_, err := page.Eval(`()=>{document.querySelectorAll('#library input').forEach(r=>r.style.opacity='0');document.querySelector('main').insertAdjacentHTML('beforeend','<input id=upload-method type=radio name=resume-method value=upload><label for=upload-method>Upload a resumé</label>');}`)
	require.NoError(t, err)
	recovered, err := New(Config{Platform: domain.PlatformSeek}).seekRecoverResumeLibrary(page, "approved.pdf")
	require.NoError(t, err)
	require.True(t, recovered)
	state, err := page.Eval(`()=>[window.deleted.length,!!document.getElementById('upload-method'),window.deleted[0].id]`)
	require.NoError(t, err)
	require.Equal(t, 1, state.Value.Get("0").Int())
	require.True(t, state.Value.Get("1").Bool())
	require.Equal(t, "oldest", state.Value.Get("2").String())
}
