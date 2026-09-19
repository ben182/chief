package box

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Location is a place a box can be created, as Hetzner describes it.
type Location struct {
	// Name is the identifier a box is created with, e.g. "fsn1".
	Name string
	// City and Country are for the person choosing. The country is the reason
	// this list is shown at all: a box holds the project's source and its .env
	// for the length of a run, and whether that happens inside the EU is a
	// decision rather than a default.
	City, Country string
	// NetworkZone groups locations that share a private network, e.g. "eu-central".
	NetworkZone string
}

// ServerType is one machine size, priced for the location it was listed for.
type ServerType struct {
	// Name is what a box is created with, e.g. "cx33".
	Name string
	// Description is Hetzner's own one-liner.
	Description string
	// Cores, Memory and Disk are the machine. Memory is in GB, Disk in GB.
	Cores  int
	Memory float64
	Disk   int
	// CPUType is "shared" or "dedicated". Shared is the right answer for a run:
	// it is a third of the price and a chief run is bounded by the agent's API
	// calls long before it is bounded by CPU.
	CPUType string
	// Architecture is "x86" or "arm". Only x86 is offered, because the chief
	// binary sent to the box is cross-compiled for amd64.
	Architecture string
	// Deprecated marks a generation Hetzner is retiring. It still creates today
	// and stops without warning, which is exactly the surprise a run four hours
	// in does not need.
	Deprecated bool
	// HourlyEUR is what the machine costs per hour, including VAT, in the
	// location it was listed for. The IPv4 address is billed separately and is
	// not in this number: 0.08 cents an hour, about two cents a day, under half
	// a cent for a five-hour run. Going without it is not an option — GitHub has
	// no IPv6 at all, and a box that cannot reach GitHub cannot clone, push, or
	// open a pull request.
	HourlyEUR float64
}

// Estimate is what a type costs for a run of the given length, which is the
// number that actually decides anything. Hetzner bills by the hour but caps at
// the monthly price; a run never gets near the cap.
func (t ServerType) Estimate(hours float64) float64 { return t.HourlyEUR * hours }

// Catalog is what can be created right now, which is not what the price list
// says. A superseded generation keeps its price entry long after the last
// location stopped offering it, so availability is asked for separately and the
// two are joined here.
type Catalog struct {
	Locations []Location
	// Types holds the machines available in each location, keyed by location
	// name, cheapest first.
	Types map[string][]ServerType
}

// TypesIn returns what can be created in a location, cheapest first.
func (c Catalog) TypesIn(location string) []ServerType { return c.Types[location] }

// Cheapest is the least machine a location will sell right now, which is what a
// box is created with when nobody named a type.
//
// A deprecated generation is skipped even when it is the cheapest line on the
// list. It creates today and stops creating without warning — the failure mode
// this whole catalog exists to avoid — and the few tenths of a cent an hour it
// saves are not worth a default that breaks on a morning nobody chose.
func (c Catalog) Cheapest(location string) (ServerType, bool) {
	types := c.TypesIn(location)
	for _, t := range types {
		if !t.Deprecated {
			return t, true
		}
	}
	// Everything here is being retired. Still better than nothing: the box has
	// to be created on something, and the list is what Hetzner says it will sell.
	if len(types) > 0 {
		return types[0], true
	}
	return ServerType{}, false
}

// Spec is the one-line shape of a machine, for the report that names the one a
// box is about to be created on. The five-hour figure is there because it is
// the number that actually decides anything: a run is an evening, and an hourly
// rate in tenths of a cent is not a quantity anybody holds in their head.
func (t ServerType) Spec() string {
	s := fmt.Sprintf("%d vCPU, %g GB, %s disk", t.Cores, t.Memory, formatGB(t.Disk))
	if t.HourlyEUR > 0 {
		s += fmt.Sprintf(" — %s an hour, %s for a five-hour run",
			FormatRateEUR(t.HourlyEUR), FormatRateEUR(t.Estimate(5)))
	}
	return s
}

// formatGB renders a disk size, in the unit somebody thinks about it in.
func formatGB(gb int) string {
	if gb >= 1000 && gb%1000 == 0 {
		return fmt.Sprintf("%d TB", gb/1000)
	}
	return fmt.Sprintf("%d GB", gb)
}

// catalog asks Hetzner what it will actually sell, in three calls: the places,
// the machines with their prices, and which machines each place still offers.
func (h *hetzner) catalog(ctx context.Context) (Catalog, error) {
	var locations struct {
		Locations []struct {
			Name        string `json:"name"`
			City        string `json:"city"`
			Country     string `json:"country"`
			NetworkZone string `json:"network_zone"`
		} `json:"locations"`
	}
	if err := h.do(ctx, http.MethodGet, "/locations", nil, &locations); err != nil {
		return Catalog{}, err
	}

	var datacenters struct {
		Datacenters []struct {
			Location struct {
				Name string `json:"name"`
			} `json:"location"`
			ServerTypes struct {
				Available []int64 `json:"available"`
			} `json:"server_types"`
		} `json:"datacenters"`
	}
	if err := h.do(ctx, http.MethodGet, "/datacenters", nil, &datacenters); err != nil {
		return Catalog{}, err
	}

	var types struct {
		ServerTypes []struct {
			ID           int64       `json:"id"`
			Name         string      `json:"name"`
			Description  string      `json:"description"`
			Cores        int         `json:"cores"`
			Memory       float64     `json:"memory"`
			Disk         int         `json:"disk"`
			CPUType      string      `json:"cpu_type"`
			Architecture string      `json:"architecture"`
			Deprecated   bool        `json:"deprecated"`
			Prices       []typePrice `json:"prices"`
		} `json:"server_types"`
	}
	if err := h.do(ctx, http.MethodGet, "/server_types?per_page=50", nil, &types); err != nil {
		return Catalog{}, err
	}

	// Which machine IDs each location will still create. A location has several
	// datacenters and a type offered by any of them can be created there.
	availableIn := map[string]map[int64]bool{}
	for _, dc := range datacenters.Datacenters {
		name := dc.Location.Name
		if availableIn[name] == nil {
			availableIn[name] = map[int64]bool{}
		}
		for _, id := range dc.ServerTypes.Available {
			availableIn[name][id] = true
		}
	}

	c := Catalog{Types: map[string][]ServerType{}}
	for _, l := range locations.Locations {
		for _, t := range types.ServerTypes {
			// arm is skipped rather than shown: the chief binary the box runs is
			// cross-compiled for amd64, so an arm box would provision perfectly
			// and then refuse to execute the one thing it was created for.
			if t.Architecture != "x86" || !availableIn[l.Name][t.ID] {
				continue
			}
			price, ok := hourlyIn(t.Prices, l.Name)
			if !ok {
				continue
			}
			c.Types[l.Name] = append(c.Types[l.Name], ServerType{
				Name:         t.Name,
				Description:  t.Description,
				Cores:        t.Cores,
				Memory:       t.Memory,
				Disk:         t.Disk,
				CPUType:      t.CPUType,
				Architecture: t.Architecture,
				Deprecated:   t.Deprecated,
				HourlyEUR:    price,
			})
		}
		if len(c.Types[l.Name]) == 0 {
			// A location that can create nothing chief can use is not a location
			// worth offering.
			continue
		}
		sort.Slice(c.Types[l.Name], func(i, j int) bool {
			return c.Types[l.Name][i].HourlyEUR < c.Types[l.Name][j].HourlyEUR
		})
		c.Locations = append(c.Locations, Location{
			Name: l.Name, City: l.City, Country: l.Country, NetworkZone: l.NetworkZone,
		})
	}

	if len(c.Locations) == 0 {
		return Catalog{}, fmt.Errorf("hetzner offered no location this project could use")
	}
	// EU first, and within that alphabetically: the default is a German region,
	// and the list should open on the neighbourhood the default lives in.
	sort.SliceStable(c.Locations, func(i, j int) bool {
		a, b := c.Locations[i], c.Locations[j]
		if inEU(a.Country) != inEU(b.Country) {
			return inEU(a.Country)
		}
		return a.Name < b.Name
	})
	return c, nil
}

// typePrice is what one server type costs in one location. Hetzner prices per
// location, so a type has one of these for each place it is offered.
type typePrice struct {
	Location    string `json:"location"`
	PriceHourly struct {
		// Gross includes VAT at the account's rate, which is what actually
		// leaves the bank account and therefore the number worth showing.
		Gross string `json:"gross"`
	} `json:"price_hourly"`
}

// hourlyIn picks a type's price in one location out of its price list.
func hourlyIn(prices []typePrice, location string) (float64, bool) {
	for _, p := range prices {
		if p.Location != location {
			continue
		}
		// Hetzner sends money as a decimal string, which is the right choice on
		// their side and means parsing on this one.
		v, err := strconv.ParseFloat(strings.TrimSpace(p.PriceHourly.Gross), 64)
		if err != nil {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

// inEU reports whether a location's country is in the EU, which is the line
// that matters when the thing being shipped to it is somebody's source code and
// their .env.
func inEU(country string) bool {
	switch strings.ToUpper(strings.TrimSpace(country)) {
	case "AT", "BE", "BG", "HR", "CY", "CZ", "DK", "EE", "FI", "FR", "DE", "GR",
		"HU", "IE", "IT", "LV", "LT", "LU", "MT", "NL", "PL", "PT", "RO", "SK",
		"SI", "ES", "SE":
		return true
	}
	return false
}

// FetchCatalog asks Hetzner what it will create right now, using the stored
// token. It is what the setup picker is built from — a list assembled from the
// API rather than one hard-coded here, because a hard-coded list is wrong the
// week a generation is retired and nobody notices until a run fails.
func FetchCatalog(ctx context.Context) (Catalog, error) {
	token, err := resolveHetznerToken()
	if err != nil {
		return Catalog{}, err
	}
	return newHetzner(token).catalog(ctx)
}
