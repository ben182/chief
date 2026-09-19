package box

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"
)

// chiefLabel is the label every box carries, and the only way a box somebody
// forgot can be found again.
const chiefLabel = "managed-by=chief"

// Running is one box in the Hetzner project, as the API sees it rather than as
// a checkout remembers it.
type Running struct {
	Name     string
	IP       string
	Type     string
	Location string
	Age      time.Duration
	// CostEUR is what it has cost since it was created, or -1 when the price of
	// that machine in that location could not be worked out.
	CostEUR float64
	// Current says this is the box the project the command ran in is using. The
	// ones without it are the interesting ones.
	Current bool
}

// List reports every box in the Hetzner project, newest first.
//
// This is the answer to the one failure mode the rest of the package cannot
// help with. Everything else — creating, watching, destroying — works from a
// record inside one checkout, and a box is forgotten precisely when that record
// stops being consulted: the branch was deleted, the laptop was reinstalled,
// the run was started three weeks ago from a directory nobody has opened since.
// The machine keeps billing regardless, and the only thing that still knows
// about it is Hetzner.
func List(ctx context.Context, baseDir string, out io.Writer) error {
	token, err := resolveHetznerToken()
	if err != nil {
		return err
	}
	return listWith(ctx, baseDir, out, newHetzner(token))
}

// listWith is List with the API client injected, so a test can check what the
// list says about a two-day-old box without owning one.
func listWith(ctx context.Context, baseDir string, out io.Writer, api *hetzner) error {
	servers, err := api.listServers(ctx, chiefLabel)
	if err != nil {
		return err
	}

	rep := reporter{out: out}
	if len(servers) == 0 {
		rep.step("No boxes — nothing is billing")
		return nil
	}

	// Prices are best-effort. A box that cannot be priced is still a box worth
	// showing, and refusing to list anything because the price list did not
	// answer would defeat the point.
	var catalog Catalog
	if c, err := api.catalog(ctx); err == nil {
		catalog = c
	}

	current, hasCurrent := LoadState(baseDir)
	boxes := make([]Running, 0, len(servers))
	var total float64
	for _, s := range servers {
		age := time.Since(s.Created).Round(time.Minute)
		r := Running{
			Name:     s.Name,
			IP:       s.IP(),
			Type:     s.Type.Name,
			Location: s.Location.Name,
			Age:      age,
			CostEUR:  -1,
			Current:  hasCurrent && s.ID == current.ServerID,
		}
		if hourly, ok := priceOf(catalog, r.Location, r.Type); ok {
			r.CostEUR = hourly * age.Hours()
			total += r.CostEUR
		}
		boxes = append(boxes, r)
	}
	sort.Slice(boxes, func(i, j int) bool { return boxes[i].Age < boxes[j].Age })

	var priced int
	for _, b := range boxes {
		if b.CostEUR >= 0 {
			priced++
		}
	}
	if priced == 0 {
		// Every price lookup failed. "0 cents so far" would read as free, which
		// is the one thing this command must never imply.
		rep.step("%s, cost unknown", plural(len(boxes), "box", "boxes"))
	} else {
		rep.step("%s, %s so far", plural(len(boxes), "box", "boxes"), FormatEUR(total))
	}
	for _, b := range boxes {
		marker := ""
		if b.Current {
			marker = "  ← this project"
		}
		rep.detail("%-34s %-6s %-6s %8s  %8s%s",
			b.Name, b.Type, b.Location, FormatAge(b.Age), FormatEUR(b.CostEUR), marker)
	}

	// Only worth saying when there is something here the current checkout cannot
	// destroy on its own.
	if len(boxes) > 1 || (len(boxes) == 1 && !boxes[0].Current) {
		rep.detail("")
		rep.detail("A box this project does not own is destroyed from the checkout that")
		rep.detail("created it, or in the Hetzner console.")
	}
	return nil
}

// priceOf finds what a machine costs per hour where it is running.
func priceOf(c Catalog, location, typeName string) (float64, bool) {
	for _, t := range c.TypesIn(location) {
		if t.Name == typeName {
			return t.HourlyEUR, true
		}
	}
	return 0, false
}

// FormatEUR renders money the way the list needs it: cents while it is cents,
// because a column of "€0.00" tells nobody anything, and an unknown price as a
// dash rather than a zero.
func FormatEUR(eur float64) string {
	switch {
	case eur < 0:
		return "—"
	case eur < 0.10:
		return fmt.Sprintf("%.0f cents", eur*100)
	default:
		return fmt.Sprintf("€%.2f", eur)
	}
}

// FormatRateEUR renders a price rather than a total: what a machine costs per
// hour, or what five hours of it would come to.
//
// One decimal more than FormatEUR, and that decimal is the whole point. Every
// machine worth choosing between here is under two cents an hour, and a column
// of "1 cents" against "1 cents" compares nothing.
func FormatRateEUR(eur float64) string {
	if eur < 0.10 {
		return fmt.Sprintf("%.1f cents", eur*100)
	}
	return fmt.Sprintf("€%.2f", eur)
}

// FormatAge renders a duration the way somebody reads a bill: days once there
// are days, because "51h" is a number you have to stop and convert, and the
// conversion is the moment you realise how long it has been.
func FormatAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// plural counts a thing in words, so the headline reads as a sentence.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
