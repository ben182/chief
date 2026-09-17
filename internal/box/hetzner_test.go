package box

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeHetzner stands in for the API. It records what it was asked for, so a
// test can check the request chief actually sends rather than only what it does
// with the answer.
type fakeHetzner struct {
	*httptest.Server
	requests []recordedRequest
}

type recordedRequest struct {
	Method string
	Path   string
	Auth   string
	Body   map[string]any
}

// newFakeHetzner starts a server that answers each path from routes. A route
// value is either a JSON string to return or a status code to fail with.
func newFakeHetzner(t *testing.T, routes map[string]any) *fakeHetzner {
	t.Helper()
	f := &fakeHetzner{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := recordedRequest{Method: r.Method, Path: r.URL.RequestURI(), Auth: r.Header.Get("Authorization")}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&rec.Body)
		}
		f.requests = append(f.requests, rec)

		key := r.Method + " " + r.URL.Path
		switch v := routes[key].(type) {
		case string:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(v))
		case int:
			w.WriteHeader(v)
			_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"not allowed"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"nope"}}`))
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// client returns a hetzner pointed at the fake.
func (f *fakeHetzner) client(token string) *hetzner {
	h := newHetzner(token)
	h.base = f.URL
	return h
}

func TestCheckTokenNamesARejectedToken(t *testing.T) {
	f := newFakeHetzner(t, map[string]any{"GET /ssh_keys": http.StatusUnauthorized})
	err := f.client("wrong").checkToken(context.Background())
	if err == nil {
		t.Fatal("expected an error for a rejected token")
	}
	// The message has to point at the thing the user can fix, not at HTTP 401.
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("error = %q, want it to say the token was rejected", err)
	}
}

func TestCheckTokenPassesAGoodOne(t *testing.T) {
	f := newFakeHetzner(t, map[string]any{"GET /ssh_keys": `{"ssh_keys":[]}`})
	if err := f.client("right").checkToken(context.Background()); err != nil {
		t.Fatalf("checkToken: %v", err)
	}
	if got := f.requests[0].Auth; got != "Bearer right" {
		t.Errorf("Authorization = %q, want the bearer token", got)
	}
}

func TestCreateServerSendsWhatHetznerNeeds(t *testing.T) {
	f := newFakeHetzner(t, map[string]any{
		"POST /servers": `{"server":{"id":42,"name":"chief-demo","status":"initializing",
			"public_net":{"ipv4":{"ip":"203.0.113.9"}}}}`,
	})

	srv, err := f.client("t").createServer(context.Background(), createServerOpts{
		Name: "chief-demo", Type: "cpx31", Image: "ubuntu-26.04", Location: "fsn1",
		SSHKeyID: 7, UserData: "#cloud-config\n", Labels: map[string]string{"managed-by": "chief"},
	})
	if err != nil {
		t.Fatalf("createServer: %v", err)
	}
	if srv.ID != 42 || srv.IP() != "203.0.113.9" {
		t.Errorf("got id=%d ip=%q, want 42 and 203.0.113.9", srv.ID, srv.IP())
	}

	body := f.requests[0].Body
	if body["name"] != "chief-demo" || body["server_type"] != "cpx31" || body["image"] != "ubuntu-26.04" {
		t.Errorf("request body = %v", body)
	}
	// Without this the instance is created but never booted, and every wait
	// after it times out on a machine that was told to sit still.
	if body["start_after_create"] != true {
		t.Errorf("start_after_create = %v, want true", body["start_after_create"])
	}
	// The key has to arrive as a list of IDs; a bare ID is silently ignored and
	// the box comes up with nobody able to log in.
	keys, ok := body["ssh_keys"].([]any)
	if !ok || len(keys) != 1 || keys[0].(float64) != 7 {
		t.Errorf("ssh_keys = %v, want [7]", body["ssh_keys"])
	}
	if !strings.HasPrefix(body["user_data"].(string), "#cloud-config") {
		t.Errorf("user_data does not look like a cloud-config")
	}
}

func TestDeleteServerTreatsAMissingOneAsDone(t *testing.T) {
	// A box someone already removed in the console is not an error: the point of
	// the call is that it no longer exists, and refusing here would leave the
	// project permanently unable to forget it.
	f := newFakeHetzner(t, map[string]any{})
	if err := f.client("t").deleteServer(context.Background(), 404); err != nil {
		t.Errorf("deleting a server that is already gone: %v", err)
	}
}

func TestDeleteServerReportsRealFailures(t *testing.T) {
	f := newFakeHetzner(t, map[string]any{"DELETE /servers/9": http.StatusForbidden})
	if err := f.client("t").deleteServer(context.Background(), 9); err == nil {
		t.Error("expected an error when the API refuses")
	}
}

func TestAPIErrorCarriesHetznersOwnMessage(t *testing.T) {
	f := newFakeHetzner(t, map[string]any{"GET /ssh_keys": http.StatusForbidden})
	_, err := f.client("t").sshKeys(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("error = %q, want Hetzner's own message in it", err)
	}
}

func TestFingerprintMatchesHetznersFormat(t *testing.T) {
	// A known ed25519 key and the MD5 fingerprint Hetzner reports for it, which
	// is what an already-registered key is recognised by.
	const key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGb9ECWmEzf8OL0M97WMHpD0nIgZ5iTn0Ttq3wRpzhpe test@example"
	got, err := fingerprint(key)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	// 16 hex pairs joined by colons.
	parts := strings.Split(got, ":")
	if len(parts) != 16 {
		t.Fatalf("fingerprint %q has %d parts, want 16", got, len(parts))
	}
	for _, p := range parts {
		if len(p) != 2 {
			t.Errorf("fingerprint part %q is not a hex pair (%s)", p, got)
		}
	}
	// Stable across calls, or key matching would be a coin flip.
	again, _ := fingerprint(key)
	if again != got {
		t.Errorf("fingerprint is not deterministic: %q then %q", got, again)
	}
	// A comment on the key must not change it — the same key added twice under
	// different names has to be recognised as one key.
	noComment, _ := fingerprint("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGb9ECWmEzf8OL0M97WMHpD0nIgZ5iTn0Ttq3wRpzhpe")
	if noComment != got {
		t.Errorf("the comment changed the fingerprint: %q vs %q", noComment, got)
	}
}

func TestFingerprintRejectsWhatIsNotAKey(t *testing.T) {
	for _, in := range []string{"", "ssh-ed25519", "ssh-ed25519 not-base64!!"} {
		if _, err := fingerprint(in); err == nil {
			t.Errorf("fingerprint(%q) succeeded, want an error", in)
		}
	}
}

func TestFindServerReportsAbsence(t *testing.T) {
	f := newFakeHetzner(t, map[string]any{"GET /servers": `{"servers":[]}`})
	_, found, err := f.client("t").findServer(context.Background(), "chief-demo")
	if err != nil {
		t.Fatalf("findServer: %v", err)
	}
	if found {
		t.Error("found a server the project does not have")
	}
}

func TestUnsupportedCombinationRecognisesARefusedTypeAndLocation(t *testing.T) {
	// The one API failure with a specific answer: the price list still
	// advertises superseded generations, so this reads like a bug in chief
	// unless it is turned into "here is what that location does offer".
	cases := []struct {
		err  error
		want bool
	}{
		{apiError{Status: 400, Code: "invalid_input", Message: "unsupported location for server type"}, true},
		{apiError{Status: 400, Code: "resource_unavailable", Message: "no resources"}, true},
		{apiError{Status: 400, Code: "invalid_input", Message: "server type unavailable in this location"}, true},
		{apiError{Status: 401, Code: "unauthorized", Message: "bad token"}, false},
		{errors.New("something else"), false},
	}
	for _, tc := range cases {
		if got := unsupportedCombination(tc.err); got != tc.want {
			t.Errorf("unsupportedCombination(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestAvailableTypesResolvesWhatALocationActuallyOffers(t *testing.T) {
	f := newFakeHetzner(t, map[string]any{
		"GET /datacenters": `{"datacenters":[
			{"name":"fsn1-dc14","location":{"name":"fsn1"},"server_types":{"available":[100,102]}},
			{"name":"nbg1-dc3","location":{"name":"nbg1"},"server_types":{"available":[101]}}
		]}`,
		"GET /server_types": `{"server_types":[
			{"id":100,"name":"cx33","cores":4,"memory":8,"architecture":"x86"},
			{"id":101,"name":"cx23","cores":2,"memory":4,"architecture":"x86"},
			{"id":102,"name":"cax31","cores":8,"memory":16,"architecture":"arm"}
		]}`,
	})

	got, err := f.client("t").availableTypes(context.Background(), "fsn1")
	if err != nil {
		t.Fatalf("availableTypes: %v", err)
	}
	// Only what fsn1 offers, and only what the box's own x86 binary can run on.
	if len(got) != 1 || !strings.HasPrefix(got[0], "cx33") {
		t.Errorf("availableTypes = %v, want just cx33", got)
	}
	// The size has to be in there: the whole point is choosing a replacement.
	if !strings.Contains(got[0], "4C") || !strings.Contains(got[0], "8GB") {
		t.Errorf("availableTypes = %v, want the size alongside the name", got)
	}
}

func TestAvailableTypesReportsALocationThatIsNotThere(t *testing.T) {
	f := newFakeHetzner(t, map[string]any{
		"GET /datacenters": `{"datacenters":[{"name":"fsn1-dc14","location":{"name":"fsn1"},"server_types":{"available":[100]}}]}`,
	})
	if _, err := f.client("t").availableTypes(context.Background(), "atlantis"); err == nil {
		t.Error("expected an error for a location that does not exist")
	}
}
