package box

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Hetzner identifies SSH keys by MD5 fingerprint; this is not a security decision
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// hetznerAPI is the Hetzner Cloud API root.
//
// Talking to it over net/http rather than through the official SDK is a
// deliberate trade. Creating and destroying a machine is four endpoints, and the
// SDK brings a Prometheus client and protobuf along for them — 69 packages into
// a binary whose entire dependency list today is the terminal UI. The cost of
// the handful of structs below is lower than the cost of that.
const hetznerAPI = "https://api.hetzner.cloud/v1"

// hetznerTimeout bounds a single API call. Every one of them is a small JSON
// request; a minute is already generous, and without it a hung connection would
// stall a run's start with no explanation.
const hetznerTimeout = 60 * time.Second

// hetzner talks to one Hetzner Cloud project.
type hetzner struct {
	token string
	// base is the API root. A field rather than the constant so a test can point
	// it at a server that answers without creating anything.
	base   string
	client *http.Client
}

func newHetzner(token string) *hetzner {
	return &hetzner{token: token, base: hetznerAPI, client: &http.Client{Timeout: hetznerTimeout}}
}

// server is the part of a Hetzner server this package uses.
type server struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Public struct {
		IPv4 struct {
			IP string `json:"ip"`
		} `json:"ipv4"`
	} `json:"public_net"`
}

// IP returns the server's public IPv4 address.
func (s server) IP() string { return s.Public.IPv4.IP }

// sshKey is a key registered in the Hetzner project.
type sshKey struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
}

// apiError is what Hetzner returns instead of a result. Its message is written
// for a person, so it is passed through rather than translated.
type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e apiError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("hetzner: %s (%s)", e.Message, e.Code)
	}
	return fmt.Sprintf("hetzner: %s", e.Message)
}

// unauthorized reports whether the call failed because the token is wrong,
// which is worth telling apart from every other failure: it is the one the user
// fixes in their own configuration rather than by retrying.
func (e apiError) unauthorized() bool {
	return e.Status == http.StatusUnauthorized || e.Code == "unauthorized" || e.Code == "forbidden"
}

// do performs one API call, encoding body as JSON when it is not nil and
// decoding the response into out when that is not nil.
func (h *hetzner) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("hetzner: encoding the request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, h.base+path, reader)
	if err != nil {
		return fmt.Errorf("hetzner: building the request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("hetzner: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("hetzner: reading the response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var wrapper struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		e := apiError{Status: resp.StatusCode, Message: strings.TrimSpace(string(data))}
		if json.Unmarshal(data, &wrapper) == nil && wrapper.Error.Message != "" {
			e.Code, e.Message = wrapper.Error.Code, wrapper.Error.Message
		}
		return e
	}

	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("hetzner: decoding the response: %w", err)
		}
	}
	return nil
}

// checkToken makes the cheapest authenticated call there is, so a wrong token is
// reported before anything has been created.
func (h *hetzner) checkToken(ctx context.Context) error {
	var out struct {
		SSHKeys []sshKey `json:"ssh_keys"`
	}
	if err := h.do(ctx, http.MethodGet, "/ssh_keys", nil, &out); err != nil {
		var e apiError
		if ok := asAPIError(err, &e); ok && e.unauthorized() {
			return fmt.Errorf("the Hetzner token was rejected — check CHIEF_BOX_HETZNER_TOKEN")
		}
		return err
	}
	return nil
}

// asAPIError unwraps err into an apiError, reporting whether it was one.
func asAPIError(err error, target *apiError) bool {
	e, ok := err.(apiError) //nolint:errorlint // do returns apiError by value and never wraps it
	if ok {
		*target = e
	}
	return ok
}

// sshKeys lists the keys registered in the project.
func (h *hetzner) sshKeys(ctx context.Context) ([]sshKey, error) {
	var out struct {
		SSHKeys []sshKey `json:"ssh_keys"`
	}
	if err := h.do(ctx, http.MethodGet, "/ssh_keys", nil, &out); err != nil {
		return nil, err
	}
	return out.SSHKeys, nil
}

// createSSHKey registers a public key under name.
func (h *hetzner) createSSHKey(ctx context.Context, name, publicKey string) (sshKey, error) {
	var out struct {
		SSHKey sshKey `json:"ssh_key"`
	}
	body := map[string]string{"name": name, "public_key": strings.TrimSpace(publicKey)}
	if err := h.do(ctx, http.MethodPost, "/ssh_keys", body, &out); err != nil {
		return sshKey{}, err
	}
	return out.SSHKey, nil
}

// createServerOpts describes the instance to create.
type createServerOpts struct {
	Name     string
	Type     string
	Image    string
	Location string
	SSHKeyID int64
	UserData string
	Labels   map[string]string
	// FirewallID is the firewall to create the server behind. Attaching it here
	// rather than afterwards matters: a server attached in a second call is
	// unprotected for the seconds between the two, and it is booting and
	// installing packages in exactly those seconds.
	FirewallID int64
}

// createServer creates the instance and returns it once Hetzner has accepted
// the request. The server is not booted yet at that point — waiting for it to
// be reachable is the caller's job, because only the caller knows what
// "reachable" means for what it is about to do.
func (h *hetzner) createServer(ctx context.Context, opts createServerOpts) (server, error) {
	body := map[string]any{
		"name":               opts.Name,
		"server_type":        opts.Type,
		"image":              opts.Image,
		"location":           opts.Location,
		"ssh_keys":           []int64{opts.SSHKeyID},
		"user_data":          opts.UserData,
		"start_after_create": true,
	}
	if len(opts.Labels) > 0 {
		body["labels"] = opts.Labels
	}
	if opts.FirewallID != 0 {
		body["firewalls"] = []map[string]int64{{"firewall": opts.FirewallID}}
	}

	var out struct {
		Server server `json:"server"`
	}
	if err := h.do(ctx, http.MethodPost, "/servers", body, &out); err != nil {
		return server{}, err
	}
	return out.Server, nil
}

// deleteServer destroys the instance. A server that is already gone is not an
// error: the point of the call is that it no longer exists.
func (h *hetzner) deleteServer(ctx context.Context, id int64) error {
	err := h.do(ctx, http.MethodDelete, "/servers/"+strconv.FormatInt(id, 10), nil, nil)
	var e apiError
	if asAPIError(err, &e) && e.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// availableTypes lists the server types that can actually be created in a
// location right now.
//
// The price list is not the answer: a type is listed with a location's price
// long after that location stopped offering it, and a superseded generation
// disappears from what can be booked while still looking perfectly available.
// The datacenters endpoint is the one that knows, and it answers in type IDs,
// so this resolves them to names a person can pass back in.
func (h *hetzner) availableTypes(ctx context.Context, location string) ([]string, error) {
	var dcs struct {
		Datacenters []struct {
			Name     string `json:"name"`
			Location struct {
				Name string `json:"name"`
			} `json:"location"`
			ServerTypes struct {
				Available []int64 `json:"available"`
			} `json:"server_types"`
		} `json:"datacenters"`
	}
	if err := h.do(ctx, http.MethodGet, "/datacenters", nil, &dcs); err != nil {
		return nil, err
	}

	available := map[int64]bool{}
	for _, dc := range dcs.Datacenters {
		if dc.Location.Name == location {
			for _, id := range dc.ServerTypes.Available {
				available[id] = true
			}
		}
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("no datacenter in %s", location)
	}

	var types struct {
		ServerTypes []struct {
			ID           int64   `json:"id"`
			Name         string  `json:"name"`
			Cores        int     `json:"cores"`
			Memory       float64 `json:"memory"`
			Architecture string  `json:"architecture"`
		} `json:"server_types"`
	}
	if err := h.do(ctx, http.MethodGet, "/server_types?per_page=50", nil, &types); err != nil {
		return nil, err
	}

	var out []string
	for _, t := range types.ServerTypes {
		if available[t.ID] && t.Architecture == "x86" {
			out = append(out, fmt.Sprintf("%s (%dC, %.0fGB)", t.Name, t.Cores, t.Memory))
		}
	}
	return out, nil
}

// unsupportedCombination reports whether err is Hetzner refusing a server type
// in a location that does not offer it — the one API failure with a specific,
// actionable answer rather than a generic one.
func unsupportedCombination(err error) bool {
	var e apiError
	if !asAPIError(err, &e) {
		return false
	}
	return strings.Contains(e.Message, "unsupported location") ||
		strings.Contains(e.Message, "unavailable") ||
		e.Code == "resource_unavailable"
}

// findServer looks a server up by name, returning false when the project has
// none by that name — which is what a box someone deleted in the console looks
// like from here.
func (h *hetzner) findServer(ctx context.Context, name string) (server, bool, error) {
	var out struct {
		Servers []server `json:"servers"`
	}
	if err := h.do(ctx, http.MethodGet, "/servers?name="+name, nil, &out); err != nil {
		return server{}, false, err
	}
	for _, s := range out.Servers {
		if s.Name == name {
			return s, true, nil
		}
	}
	return server{}, false, nil
}

// fingerprint computes the MD5 fingerprint Hetzner identifies a public key by:
// the digest of the raw key blob, printed as colon-separated hex. It is how an
// already-registered key is recognised without relying on its name, which is
// whatever the person who uploaded it typed.
func fingerprint(publicKey string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(publicKey))
	if len(fields) < 2 {
		return "", fmt.Errorf("not an SSH public key")
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return "", fmt.Errorf("not an SSH public key: %w", err)
	}
	sum := md5.Sum(blob) //nolint:gosec // the format Hetzner reports, not a security choice
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(parts, ":"), nil
}

// firewall is a Hetzner firewall, which is the cheapest security a box can
// have: it is free, it is enforced outside the machine, and it does not care
// what the run does to iptables inside.
type firewall struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// createFirewall makes the firewall a box runs behind.
//
// The rules are an allowlist and they are deliberately short: SSH from
// anywhere, ping from anywhere, nothing else in. SSH stays open to the world
// rather than pinned to the creator's address, because an address that changes
// between creating a box and looking at it locks you out of your own machine
// for no gain — the port is protected by keys, not by obscurity.
//
// What this does close is everything else, and that is the point. A project's
// `worktree.setup` starts a dev server, a queue dashboard, a database that was
// configured to listen on all interfaces; without a firewall each of those is
// on the public internet for the hours the run takes, and nobody involved
// decided that. Outbound is untouched — no `out` rules means Hetzner allows all
// of it, which apt, GitHub and the agent's API all need.
func (h *hetzner) createFirewall(ctx context.Context, name string) (firewall, error) {
	anywhere := []string{"0.0.0.0/0", "::/0"}
	body := map[string]any{
		"name": name,
		"rules": []map[string]any{
			{
				"direction":   "in",
				"protocol":    "tcp",
				"port":        "22",
				"source_ips":  anywhere,
				"description": "ssh",
			},
			{
				"direction":   "in",
				"protocol":    "icmp",
				"source_ips":  anywhere,
				"description": "ping",
			},
		},
	}

	var out struct {
		Firewall firewall `json:"firewall"`
	}
	if err := h.do(ctx, http.MethodPost, "/firewalls", body, &out); err != nil {
		return firewall{}, err
	}
	return out.Firewall, nil
}

// deleteFirewall removes a firewall once the box behind it is gone. One that is
// already deleted is not an error, for the same reason a deleted server is not.
func (h *hetzner) deleteFirewall(ctx context.Context, id int64) error {
	err := h.do(ctx, http.MethodDelete, "/firewalls/"+strconv.FormatInt(id, 10), nil, nil)
	var e apiError
	if asAPIError(err, &e) && e.Status == http.StatusNotFound {
		return nil
	}
	return err
}
