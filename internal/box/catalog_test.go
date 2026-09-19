package box

import (
	"context"
	"net/http"
	"testing"
)

// catalogRoutes is one plausible answer from Hetzner: two locations, one of
// them outside the EU, and four machines of which only some are creatable.
func catalogRoutes() map[string]any {
	return map[string]any{
		"GET /locations": `{"locations":[
			{"name":"ash","city":"Ashburn, VA","country":"US","network_zone":"us-east"},
			{"name":"fsn1","city":"Falkenstein","country":"DE","network_zone":"eu-central"}
		]}`,
		"GET /datacenters": `{"datacenters":[
			{"location":{"name":"fsn1"},"server_types":{"available":[1,2,4]}},
			{"location":{"name":"ash"},"server_types":{"available":[2]}}
		]}`,
		"GET /server_types": `{"server_types":[
			{"id":1,"name":"cx22","cores":2,"memory":4,"disk":40,"cpu_type":"shared","architecture":"x86","deprecated":false,
			 "prices":[{"location":"fsn1","price_hourly":{"gross":"0.0060"}}]},
			{"id":2,"name":"cx33","cores":4,"memory":8,"disk":80,"cpu_type":"shared","architecture":"x86","deprecated":false,
			 "prices":[{"location":"fsn1","price_hourly":{"gross":"0.0119"}},
			           {"location":"ash","price_hourly":{"gross":"0.0143"}}]},
			{"id":3,"name":"cx99","cores":8,"memory":16,"disk":160,"cpu_type":"shared","architecture":"x86","deprecated":false,
			 "prices":[{"location":"fsn1","price_hourly":{"gross":"0.0300"}}]},
			{"id":4,"name":"cax21","cores":4,"memory":8,"disk":80,"cpu_type":"shared","architecture":"arm","deprecated":false,
			 "prices":[{"location":"fsn1","price_hourly":{"gross":"0.0080"}}]}
		]}`,
	}
}

func TestCatalogOffersOnlyWhatCanBeCreated(t *testing.T) {
	f := newFakeHetzner(t, catalogRoutes())
	c, err := f.client("t").catalog(context.Background())
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}

	var names []string
	for _, ty := range c.TypesIn("fsn1") {
		names = append(names, ty.Name)
	}
	// cx99 is priced in fsn1 but no datacenter there will create it — exactly the
	// trap the price list sets, and the reason availability is asked for
	// separately.
	for _, n := range names {
		if n == "cx99" {
			t.Error("offered cx99, which fsn1 does not actually create")
		}
		// cax21 is arm; the chief binary sent to the box is built for amd64.
		if n == "cax21" {
			t.Error("offered an arm machine, which cannot run the binary chief uploads")
		}
	}
	if len(names) != 2 {
		t.Errorf("fsn1 offers %v, want cx22 and cx33", names)
	}
}

func TestCatalogPricesPerLocation(t *testing.T) {
	f := newFakeHetzner(t, catalogRoutes())
	c, err := f.client("t").catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// The same machine costs different amounts in different places, so a price
	// carried over from another location would quietly understate the bill.
	find := func(location, name string) ServerType {
		t.Helper()
		for _, ty := range c.TypesIn(location) {
			if ty.Name == name {
				return ty
			}
		}
		t.Fatalf("%s is not offered in %s", name, location)
		return ServerType{}
	}
	if got := find("fsn1", "cx33").HourlyEUR; got != 0.0119 {
		t.Errorf("cx33 in fsn1 = %v/h, want 0.0119", got)
	}
	if got := find("ash", "cx33").HourlyEUR; got != 0.0143 {
		t.Errorf("cx33 in ash = %v/h, want 0.0143", got)
	}
}

func TestCatalogSortsCheapestFirstAndEUFirst(t *testing.T) {
	f := newFakeHetzner(t, catalogRoutes())
	c, err := f.client("t").catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// The default is a German region, so the list should open in the EU rather
	// than making someone scroll past Virginia to get to it.
	if c.Locations[0].Name != "fsn1" {
		t.Errorf("first location = %s, want the EU one first", c.Locations[0].Name)
	}
	types := c.TypesIn("fsn1")
	if types[0].Name != "cx22" {
		t.Errorf("first type = %s, want the cheapest", types[0].Name)
	}
}

func TestCatalogDropsALocationWithNothingUsable(t *testing.T) {
	routes := catalogRoutes()
	// ash offers only cx33, and now nothing is available there at all.
	routes["GET /datacenters"] = `{"datacenters":[
		{"location":{"name":"fsn1"},"server_types":{"available":[1,2]}},
		{"location":{"name":"ash"},"server_types":{"available":[]}}
	]}`
	f := newFakeHetzner(t, routes)
	c, err := f.client("t").catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range c.Locations {
		if l.Name == "ash" {
			t.Error("offered a location that can create nothing")
		}
	}
}

func TestEstimateIsTheNumberThatDecides(t *testing.T) {
	ty := ServerType{HourlyEUR: 0.0119}
	if got := ty.Estimate(5); got < 0.059 || got > 0.060 {
		t.Errorf("five hours of cx33 = %v, want about 0.0595", got)
	}
}

// TestABoxWithNoTypeGetsTheCheapest is the default this whole catalog exists
// for: a machine chosen at the moment of creation from what Hetzner will
// actually sell, rather than a name written into chief that goes stale.
func TestABoxWithNoTypeGetsTheCheapest(t *testing.T) {
	f := newFakeHetzner(t, catalogRoutes())
	machine, chosen := chooseType(context.Background(), f.client("t"), "", "fsn1")
	if machine.Name != "cx22" {
		t.Errorf("created a %s, want the cheapest fsn1 sells (cx22)", machine.Name)
	}
	if !chosen {
		t.Error("did not report the machine as chief's choice, so the report will not name it")
	}
	if machine.HourlyEUR == 0 {
		t.Error("no price came back with the machine, so the box would be recorded as free")
	}
}

// TestANamedTypeIsTakenAsGiven: a --type or a box.type is a person deciding,
// and the price is looked up rather than the decision second-guessed.
func TestANamedTypeIsTakenAsGiven(t *testing.T) {
	f := newFakeHetzner(t, catalogRoutes())
	machine, chosen := chooseType(context.Background(), f.client("t"), "cx33", "fsn1")
	if machine.Name != "cx33" {
		t.Errorf("created a %s, want the cx33 that was asked for", machine.Name)
	}
	if chosen {
		t.Error("reported a named type as chief's choice")
	}
	if machine.HourlyEUR != 0.0119 {
		t.Errorf("priced cx33 at %v, want its fsn1 price", machine.HourlyEUR)
	}
}

// TestAPriceListThatDoesNotAnswerStillCreatesABox. The catalog is how the
// default is found, but it is not worth a run: a Hetzner that is having a bad
// morning should cost the fallback machine, not the night's work.
func TestAPriceListThatDoesNotAnswerStillCreatesABox(t *testing.T) {
	f := newFakeHetzner(t, map[string]any{"GET /locations": http.StatusInternalServerError})
	machine, chosen := chooseType(context.Background(), f.client("t"), "", "fsn1")
	if machine.Name != FallbackType {
		t.Errorf("created a %s, want the fallback %s", machine.Name, FallbackType)
	}
	if chosen {
		t.Error("announced a fallback as the cheapest, which it was not established to be")
	}

	// And a named one survives the same outage, unpriced.
	machine, _ = chooseType(context.Background(), f.client("t"), "cx33", "fsn1")
	if machine.Name != "cx33" {
		t.Errorf("dropped a named type when the price list failed: %s", machine.Name)
	}
}

// TestCheapestSkipsARetiredGeneration. A deprecated line still creates today
// and stops without warning; saving two tenths of a cent an hour is not worth a
// default that breaks on a morning nobody chose.
func TestCheapestSkipsARetiredGeneration(t *testing.T) {
	c := Catalog{Types: map[string][]ServerType{"fsn1": {
		{Name: "old", HourlyEUR: 0.004, Deprecated: true},
		{Name: "current", HourlyEUR: 0.006},
	}}}
	got, ok := c.Cheapest("fsn1")
	if !ok || got.Name != "current" {
		t.Errorf("chose %q, want the cheapest that is not being retired", got.Name)
	}

	// Unless everything is being retired, in which case a box still has to be
	// created on something.
	c = Catalog{Types: map[string][]ServerType{"fsn1": {{Name: "old", Deprecated: true}}}}
	if got, ok := c.Cheapest("fsn1"); !ok || got.Name != "old" {
		t.Errorf("chose %q from a list of nothing but retired machines", got.Name)
	}

	if _, ok := c.Cheapest("nowhere"); ok {
		t.Error("claimed a machine in a location that sells none")
	}
}
