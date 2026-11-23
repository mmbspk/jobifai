package bot

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLinkedInAdaptiveSubmit_BoundedNativeClickWithOwnedDecoration(t *testing.T) {
	page := rodTestPage(t, `<html><head><style>button{position:relative;width:180px;height:44px}button::after{content:'';position:absolute;inset:0}</style></head><body><dialog open><div data-sdui-screen="EasyApply"><button id=submit>Submit application</button></div></dialog><script>window.clicks=[];document.getElementById('submit').onclick=e=>window.clicks.push(e.isTrusted);</script></body></html>`)
	require.NoError(t, clickLinkedInAdaptiveSubmit(context.Background(), page, nil))
	result, err := page.Eval(`()=>window.clicks`)
	require.NoError(t, err)
	require.Len(t, result.Value.Arr(), 1)
	require.True(t, result.Value.Get("0").Bool())
}

func TestLinkedInNativeClickPoint_SettlesOwnedControlBeforeInput(t *testing.T) {
	page := rodTestPage(t, `<html><body><button id=action>Continue</button><script>window.clicks=0;document.getElementById('action').onclick=()=>window.clicks++;</script></body></html>`)
	button := page.MustElement("#action")
	_, err := page.Eval(`()=>{document.getElementById('action').style.opacity='0';setTimeout(()=>document.getElementById('action').style.opacity='1',100)}`)
	require.NoError(t, err)
	point, err := linkedInNativeClickPoint(context.Background(), button)
	require.NoError(t, err)
	require.NotNil(t, point)
	value, err := page.Eval(`()=>window.clicks`)
	require.NoError(t, err)
	require.Zero(t, value.Value.Int(), "settling never sends native input")
}

func TestLinkedInAdaptiveSubmit_RechecksFactsAfterVisibilitySettles(t *testing.T) {
	page := rodTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><button id=submit>Submit application</button></div></dialog><script>window.clicks=0;window.factsValid=true;document.getElementById('submit').onclick=()=>window.clicks++;</script></body></html>`)
	calls := 0
	err := clickLinkedInAdaptiveSubmit(context.Background(), page, func(context.Context) error {
		calls++
		if calls == 1 {
			_, err := page.Eval(`()=>{const button=document.getElementById('submit');button.style.opacity='0';setTimeout(()=>{window.factsValid=false;button.style.opacity='1'},100)}`)
			return err
		}
		value, err := page.Eval(`()=>window.factsValid`)
		if err != nil {
			return err
		}
		if !value.Value.Bool() {
			return errors.New("job identity, document or rollback guard changed")
		}
		return nil
	})
	require.Error(t, err)
	require.Equal(t, 2, calls)
	value, err := page.Eval(`()=>window.clicks`)
	require.NoError(t, err)
	require.Zero(t, value.Value.Int())
}

func TestLinkedInAdaptiveSubmit_RejectsUnrelatedCoverAndVerificationFailure(t *testing.T) {
	for _, cover := range []bool{false, true} {
		t.Run(map[bool]string{false: "verification changed", true: "unrelated overlay"}[cover], func(t *testing.T) {
			overlay := ""
			if cover {
				overlay = `<div style="position:absolute;inset:0;z-index:10;background:rgba(1,1,1,0.01)"></div>`
			}
			page := rodTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><button id=submit>Submit application</button>`+overlay+`</div></dialog><script>window.clicks=0;document.getElementById('submit').onclick=()=>window.clicks++;</script></body></html>`)
			err := clickLinkedInAdaptiveSubmit(context.Background(), page, func(context.Context) error { return errors.New("identity or approved document changed") })
			require.Error(t, err)
			result, err := page.Eval(`()=>window.clicks`)
			require.NoError(t, err)
			require.Zero(t, result.Value.Int())
		})
	}
}

func TestLinkedInAdaptiveSubmit_HoverReplacementHasOneMousePress(t *testing.T) {
	page := rodTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><button id=submit>Submit application</button></div></dialog><script>window.clicks=0;window.replaced=false;document.getElementById('submit').onmouseenter=()=>{if(window.replaced)return;window.replaced=true;const old=document.getElementById('submit'),fresh=old.cloneNode(true);fresh.onclick=()=>window.clicks++;old.replaceWith(fresh);};</script></body></html>`)
	require.NoError(t, clickLinkedInAdaptiveSubmit(context.Background(), page, nil))
	result, err := page.Eval(`()=>[window.replaced,window.clicks]`)
	require.NoError(t, err)
	require.True(t, result.Value.Get("0").Bool())
	require.Equal(t, 1, result.Value.Get("1").Int())
}
