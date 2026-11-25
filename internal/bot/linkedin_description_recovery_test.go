package bot

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLinkedInDescription_ExplicitJobSectionOutsideVerifiedHeader(t *testing.T) {
	br := launchTestBrowser(t)
	description := strings.Repeat("Verified responsibilities and qualifications for the intended position. ", 8)
	html := `<main><section data-job-id="123"><h1>Coordinator</h1><a aria-label="Company, Acme.">Acme</a><button>Easy Apply</button></section><section id="JobDetails_AboutTheJob_123"><p>` + description + `</p></section><aside><section id="JobDetails_AboutTheJob_999">Wrong recommendation description</section></aside></main>`
	srv := fixtureServer(t, "123", "", html, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	job := linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Coordinator", Company: "Acme"}
	r, err := readLinkedInDescription(context.Background(), page, job)
	require.NoError(t, err)
	require.True(t, r.IdentityVerified)
	require.Contains(t, r.Description, "Verified responsibilities")
	require.NotContains(t, r.Description, "Wrong recommendation")
}

func TestReadLinkedInDescription_ZeroHeightSDUISectionHydrates(t *testing.T) {
	br := launchTestBrowser(t)
	html := `<main><section data-job-id="123"><h1>Coordinator</h1><a aria-label="Company, Acme.">Acme</a><button>Easy Apply</button></section><section id="JobDetails_AboutTheJob_123" style="margin-top:1500px"></section><div style="height:1000px"></div></main><script>
 const desc=document.getElementById('JobDetails_AboutTheJob_123');
 const observer=new IntersectionObserver(entries=>{if(!entries.some(e=>e.isIntersecting))return;observer.disconnect();
 setTimeout(()=>{desc.textContent='Verified lazy responsibilities and qualifications. '.repeat(8);window.hydrated=true;},350);});observer.observe(desc);
 </script>`
	srv := fixtureServer(t, "123", "", html, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	r, err := readLinkedInDescription(context.Background(), page, linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Coordinator", Company: "Acme"})
	require.NoError(t, err)
	require.Contains(t, r.Description, "Verified lazy responsibilities")
	hydrated, err := page.Eval(`()=>window.hydrated===true`)
	require.NoError(t, err)
	require.True(t, hydrated.Value.Bool())
}

func TestReadLinkedInDescription_DelayedSDUISection(t *testing.T) {
	br := launchTestBrowser(t)
	html := fmt.Sprintf(`<main><section data-job-id="123"><h1>Coordinator</h1><a aria-label="Company, Acme.">Acme</a><button>Easy Apply</button></section><section id="JobDetails_AboutTheJob_123">Loading</section></main><script>setTimeout(()=>{document.getElementById('JobDetails_AboutTheJob_123').textContent=%q;window.hydrated=true;},2600);</script>`, strings.Repeat("Verified delayed responsibilities and qualifications. ", 8))
	srv := fixtureServer(t, "123", "", html, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	r, err := readLinkedInDescription(context.Background(), page, linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Coordinator", Company: "Acme"})
	require.NoError(t, err)
	require.True(t, r.IdentityVerified)
	require.Contains(t, r.Description, "Verified delayed responsibilities")
}

func TestReadLinkedInDescription_HydrationWaitHonorsCancellation(t *testing.T) {
	br := launchTestBrowser(t)
	html := `<main data-job-id="123"><h1>Coordinator</h1><button>Easy Apply</button><div data-job-description>Loading</div></main>`
	srv := fixtureServer(t, "123", "", html, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := readLinkedInDescription(ctx, page, linkedInJob{ID: "123", URL: srv.URL + "/jobs/view/123", Title: "Coordinator"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 3*time.Second, "cancellation must not wait for the hydration deadline")
}
