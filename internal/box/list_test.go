package box

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The fixtures below use the shape the Hetzner API actually returns: a server's
// location sits at its top level. An earlier version of these tests nested it
// under a datacenter, matching the struct rather than the API, and so agreed
// with the bug instead of catching it — the boxes listed with no location and
// no cost, and only a real box showed it.
func TestListNamesEveryBoxAndWhatItCost(t *testing.T) {
	old := time.Now().Add(-50 * time.Hour).UTC().Format(time.RFC3339)
	recent := time.Now().Add(-90 * time.Minute).UTC().Format(time.RFC3339)

	routes := catalogRoutes()
	routes["GET /servers"] = `{"servers":[
		{"id":1,"name":"chief-shop-import-0915","created":"` + old + `","server_type":{"name":"cx33"},
		 "location":{"name":"fsn1"},"public_net":{"ipv4":{"ip":"203.0.113.1"}}},
		{"id":2,"name":"chief-chief-auth-1422","created":"` + recent + `","server_type":{"name":"cx22"},
		 "location":{"name":"fsn1"},"public_net":{"ipv4":{"ip":"203.0.113.2"}}}
	]}`
	f := newFakeHetzner(t, routes)
	t.Setenv("CHIEF_BOX_HETZNER_TOKEN", "token")

	var out strings.Builder
	if err := listWith(context.Background(), t.TempDir(), &out, f.client("token")); err != nil {
		t.Fatalf("List: %v", err)
	}
	got := out.String()

	// A box running for two days is the whole reason this command exists, and
	// the number that makes somebody act on it is the one in euros.
	if !strings.Contains(got, "chief-shop-import-0915") {
		t.Errorf("the forgotten box is missing:\n%s", got)
	}
	if !strings.Contains(got, "2d 2h") {
		t.Errorf("age is not readable as days:\n%s", got)
	}
	// 50h × €0.0119 = €0.60
	if !strings.Contains(got, "€0.60") {
		t.Errorf("no cost for the two-day box:\n%s", got)
	}
}

func TestListSaysNothingIsBilling(t *testing.T) {
	routes := catalogRoutes()
	routes["GET /servers"] = `{"servers":[]}`
	f := newFakeHetzner(t, routes)

	var out strings.Builder
	if err := listWith(context.Background(), t.TempDir(), &out, f.client("token")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing is billing") {
		t.Errorf("an empty project should say so plainly:\n%s", out.String())
	}
}

func TestListAsksOnlyForChiefBoxes(t *testing.T) {
	routes := catalogRoutes()
	routes["GET /servers"] = `{"servers":[]}`
	f := newFakeHetzner(t, routes)

	var out strings.Builder
	if err := listWith(context.Background(), t.TempDir(), &out, f.client("token")); err != nil {
		t.Fatal(err)
	}
	// Somebody's Hetzner project holds their real servers too. Listing those
	// under a heading about what is billing would be alarming and wrong.
	if !strings.Contains(f.requests[0].Path, "label_selector=managed-by") {
		t.Errorf("asked for every server, not only chief's: %s", f.requests[0].Path)
	}
}

func TestListMarksThisProjectsBox(t *testing.T) {
	dir := t.TempDir()
	if err := SaveState(dir, State{ServerID: 2, Name: "chief-chief-auth-1422", IP: "203.0.113.2"}); err != nil {
		t.Fatal(err)
	}
	created := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	routes := catalogRoutes()
	routes["GET /servers"] = `{"servers":[
		{"id":2,"name":"chief-chief-auth-1422","created":"` + created + `","server_type":{"name":"cx22"},
		 "location":{"name":"fsn1"},"public_net":{"ipv4":{"ip":"203.0.113.2"}}}
	]}`
	f := newFakeHetzner(t, routes)

	var out strings.Builder
	if err := listWith(context.Background(), dir, &out, f.client("token")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "this project") {
		t.Errorf("the box this checkout owns is not marked:\n%s", out.String())
	}
}

func TestListSurvivesAMissingPriceList(t *testing.T) {
	created := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	// No catalog routes at all: the price lookup fails and the boxes still have
	// to be shown, because an unlisted box is one nobody destroys.
	f := newFakeHetzner(t, map[string]any{
		"GET /servers": `{"servers":[
			{"id":1,"name":"chief-demo","created":"` + created + `","server_type":{"name":"cx22"},
			 "location":{"name":"fsn1"},"public_net":{"ipv4":{"ip":"203.0.113.1"}}}
		]}`,
	})

	var out strings.Builder
	if err := listWith(context.Background(), t.TempDir(), &out, f.client("token")); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !strings.Contains(out.String(), "chief-demo") {
		t.Errorf("dropped a box because it could not be priced:\n%s", out.String())
	}
}

func TestFormatAgeReadsLikeABill(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Minute, "45m"},
		{3*time.Hour + 12*time.Minute, "3h 12m"},
		// "51h" is a number you have to stop and convert. The conversion is the
		// moment somebody realises how long it has been.
		{51 * time.Hour, "2d 3h"},
	} {
		if got := formatAge(tc.d); got != tc.want {
			t.Errorf("formatAge(%s) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestFormatEURDoesNotShowAnUnknownPriceAsFree(t *testing.T) {
	if got := formatEUR(-1); got == "€0.00" || strings.Contains(got, "0") {
		t.Errorf("formatEUR(unknown) = %q, which reads as free", got)
	}
}

func TestGitHubTokenNoteWarnsOnlyAboutBroadTokens(t *testing.T) {
	broad := []string{"gho_abc123", "ghp_abc123", "ghu_abc123"}
	for _, token := range broad {
		if gitHubTokenNote(token) == "" {
			t.Errorf("%s reaches every repository the account does, and said nothing", token[:4])
		}
	}
	narrow := []string{"github_pat_abc123", "ghs_abc123", "", "some-enterprise-thing"}
	for _, token := range narrow {
		if note := gitHubTokenNote(token); note != "" {
			t.Errorf("warned about %q, which is scoped or unknown: %s", token, note)
		}
	}
}

// TestDownAllDestroysWhatNoCheckoutRemembers covers the gap `chief box list`
// used to leave open: it could show you a box you had forgotten and then had
// nothing to offer but a link to the Hetzner console.
func TestDownAllDestroysWhatNoCheckoutRemembers(t *testing.T) {
	created := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	routes := map[string]any{
		"GET /servers": `{"servers":[
			{"id":1,"name":"chief-shop-import-0915","created":"` + created + `","server_type":{"name":"cx23"},
			 "location":{"name":"fsn1"},"public_net":{"ipv4":{"ip":"192.0.2.1"}}},
			{"id":2,"name":"chief-chief-auth-1422","created":"` + created + `","server_type":{"name":"cx23"},
			 "location":{"name":"fsn1"},"public_net":{"ipv4":{"ip":"192.0.2.2"}}}
		]}`,
		"DELETE /servers/1": `{}`,
		"DELETE /servers/2": `{}`,
	}
	f := newFakeHetzner(t, routes)

	var out strings.Builder
	asked := ""
	err := downWith(context.Background(), DownOptions{
		BaseDir: t.TempDir(),
		All:     true,
		Out:     &out,
		Confirm: func(prompt string) bool { asked = prompt; return true },
	}, f.client("token"))
	if err != nil {
		t.Fatalf("down --all: %v", err)
	}

	if !strings.Contains(asked, "2 boxes") {
		t.Errorf("the question did not say how many machines it covers: %q", asked)
	}
	var deleted []string
	for _, r := range f.requests {
		if r.Method == http.MethodDelete {
			deleted = append(deleted, r.Path)
		}
	}
	if len(deleted) != 2 {
		t.Errorf("deleted %v, want both boxes", deleted)
	}
	if !strings.Contains(out.String(), "chief-shop-import-0915") {
		t.Errorf("the report does not name what it destroyed:\n%s", out.String())
	}
}

func TestDownAllKeepsEverythingWhenTheAnswerIsNo(t *testing.T) {
	created := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	f := newFakeHetzner(t, map[string]any{
		"GET /servers": `{"servers":[
			{"id":1,"name":"chief-demo-0915","created":"` + created + `","server_type":{"name":"cx23"},
			 "location":{"name":"fsn1"},"public_net":{"ipv4":{"ip":"192.0.2.1"}}}
		]}`,
		"DELETE /servers/1": `{}`,
	})

	var out strings.Builder
	err := downWith(context.Background(), DownOptions{
		BaseDir: t.TempDir(),
		All:     true,
		Out:     &out,
		Confirm: func(string) bool { return false },
	}, f.client("token"))
	if err == nil {
		t.Fatal("a refused destroy reported success")
	}
	for _, r := range f.requests {
		if r.Method == http.MethodDelete {
			t.Errorf("a box was destroyed after the answer was no: %s", r.Path)
		}
	}
}

func TestDownByNameLeavesTheOtherBoxesAlone(t *testing.T) {
	created := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	f := newFakeHetzner(t, map[string]any{
		"GET /servers": `{"servers":[
			{"id":1,"name":"chief-keep-0915","created":"` + created + `","server_type":{"name":"cx23"},
			 "location":{"name":"fsn1"},"public_net":{"ipv4":{"ip":"192.0.2.1"}}},
			{"id":2,"name":"chief-go-1422","created":"` + created + `","server_type":{"name":"cx23"},
			 "location":{"name":"fsn1"},"public_net":{"ipv4":{"ip":"192.0.2.2"}}}
		]}`,
		"DELETE /servers/2": `{}`,
	})

	var out strings.Builder
	err := downWith(context.Background(), DownOptions{
		BaseDir: t.TempDir(),
		Name:    "chief-go-1422",
		Force:   true,
		Out:     &out,
	}, f.client("token"))
	if err != nil {
		t.Fatalf("down --name: %v", err)
	}
	for _, r := range f.requests {
		if r.Method == http.MethodDelete && !strings.HasSuffix(r.Path, "/servers/2") {
			t.Errorf("a box that was not named got destroyed: %s", r.Path)
		}
	}

	// A name nobody has is a typo, and deleting nothing quietly would read as
	// success.
	err = downWith(context.Background(), DownOptions{
		BaseDir: t.TempDir(), Name: "chief-typo", Force: true, Out: &out,
	}, f.client("token"))
	if err == nil || !strings.Contains(err.Error(), "chief-typo") {
		t.Errorf("a name that matches nothing reported %v", err)
	}
}
