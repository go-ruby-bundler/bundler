// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-ruby-rubygems/rubygems"
)

// IndexSpec is one candidate gem version in the resolution index: its name,
// version, and its own runtime dependencies. It is the unit the resolver
// activates. Where the candidate comes from (network, mirror, fixture) is a
// host-side seam.
type IndexSpec struct {
	Name         string
	Version      *rubygems.Version
	Platform     string
	Dependencies []SpecDependency
}

// Index is the resolution seam: it answers "what versions of NAME exist, and
// what does each depend on?" with NO network and NO filesystem. A test fixture,
// an in-memory mirror of the compact index, or an HTTP-backed implementation
// can all satisfy it.
type Index interface {
	// Versions returns every available spec for name. The resolver sorts them
	// itself, so any order is fine. An unknown gem returns an empty slice.
	Versions(name string) []IndexSpec
}

// MapIndex is a trivial in-memory Index built from a name -> specs map. It is
// the fixture index used by the tests and a convenient host-side adapter.
type MapIndex map[string][]IndexSpec

// Versions implements Index.
func (m MapIndex) Versions(name string) []IndexSpec { return m[name] }

// AddGem registers a gem version and its dependencies into a MapIndex. deps are
// alternating name, constraint-string pairs flattened per gem via NewDependency
// is overkill here; callers pass SpecDependency directly through Add instead.
func (m MapIndex) AddGem(name, version string, deps ...SpecDependency) error {
	v, err := rubygems.NewVersion(version)
	if err != nil {
		return err
	}
	m[name] = append(m[name], IndexSpec{Name: name, Version: v, Dependencies: deps})
	return nil
}

// Dep is a helper to build a SpecDependency from a name and constraint strings.
func Dep(name string, constraints ...string) SpecDependency {
	return SpecDependency{Name: name, Requirement: rubygems.MustRequirement(constraints...)}
}

// VersionConflict is returned when no consistent set satisfies the
// requirements. Its message names the gem that could not be resolved and the
// conflicting requirements, in the shape Bundler reports.
type VersionConflict struct {
	// Name is the gem whose requirements conflict.
	Name string
	// Requesters maps each requirement string to the gem(s) that imposed it
	// ("Gemfile" for a root dependency).
	Requesters map[string][]string
	msg        string
}

func (e *VersionConflict) Error() string { return e.msg }

// Resolution is the successful result of Resolve: the activated specs keyed by
// name, in a deterministic order.
type Resolution struct {
	specs map[string]*Spec
}

// Specs returns the resolved specs sorted by name.
func (r *Resolution) Specs() []*Spec {
	out := make([]*Spec, 0, len(r.specs))
	for _, s := range r.specs {
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns the resolved spec for name, or nil.
func (r *Resolution) Get(name string) *Spec { return r.specs[name] }

// req is a pending requirement during resolution: a dependency plus who asked
// for it (for conflict reporting).
type req struct {
	dep       SpecDependency
	requester string // "Gemfile" or "name (version)"
}

// resolver holds the backtracking state.
type resolver struct {
	index           Index
	allowPrerelease bool
	source          *Source // attached to every produced Spec (the GEM source)
}

// Resolve resolves root dependencies against an index to a consistent set, or
// returns a *VersionConflict. The produced specs all carry the given source
// (use a GEM source for the common case). It implements depth-first backtracking
// with the most-constrained-package-first heuristic and highest-version-first
// candidate ordering, which yields Bundler's deterministic resolution.
func Resolve(deps []*Dependency, index Index, source *Source) (*Resolution, error) {
	r := &resolver{index: index, source: source}
	for _, d := range deps {
		if d.Requirement != nil && d.Requirement.Prerelease() {
			r.allowPrerelease = true
		}
	}

	initial := make([]req, 0, len(deps))
	for _, d := range deps {
		rq := d.Requirement
		if rq == nil {
			rq = rubygems.DefaultRequirement()
		}
		initial = append(initial, req{dep: SpecDependency{Name: d.Name, Requirement: rq}, requester: "Gemfile"})
	}

	activated := map[string]*activation{}
	if err := r.solve(initial, activated); err != nil {
		return nil, err
	}

	out := map[string]*Spec{}
	for name, a := range activated {
		spec := &Spec{
			Name:         name,
			Version:      a.cand.Version,
			Platform:     a.cand.Platform,
			Source:       source,
			Dependencies: a.cand.Dependencies,
		}
		out[name] = spec
	}
	return &Resolution{specs: out}, nil
}

// activation records a chosen candidate and the requirements that constrain it.
type activation struct {
	cand     IndexSpec
	requests []req // every requirement seen for this name (for conflict context)
}

// solve activates the given requirements on top of the already-activated set,
// recursing depth-first. It returns nil on success (activated is filled) or a
// *VersionConflict.
func (r *resolver) solve(pending []req, activated map[string]*activation) error {
	if len(pending) == 0 {
		return nil
	}

	// Most-constrained-first: pick the pending requirement whose package has the
	// fewest viable candidates given everything constraining it so far. This is
	// the heuristic that makes the search both fast and deterministic.
	pkgReqs := groupByName(pending)
	names := sortedKeys(pkgReqs)

	bestName := ""
	var bestCands []IndexSpec
	bestCount := -1
	for _, name := range names {
		cands := r.candidates(name, append(reqsFor(name, activated), pkgReqs[name]...))
		if bestCount == -1 || len(cands) < bestCount {
			bestCount = len(cands)
			bestName = name
			bestCands = cands
		}
	}

	reqsHere := pkgReqs[bestName]

	// If already activated, it must satisfy the new requirements; otherwise this
	// branch is dead (conflict between the active version and a new requirement).
	if a, ok := activated[bestName]; ok {
		for _, rq := range reqsHere {
			if !rq.dep.Requirement.SatisfiedBy(a.cand.Version) {
				return r.conflict(bestName, append(a.requests, reqsHere...), activated)
			}
		}
		a.requests = append(a.requests, reqsHere...)
		rest := withoutName(pending, bestName)
		err := r.solve(rest, activated)
		if err == nil {
			return nil
		}
		// undo the appended requests on failure
		a.requests = a.requests[:len(a.requests)-len(reqsHere)]
		return err
	}

	if len(bestCands) == 0 {
		return r.conflict(bestName, append(reqsFor(bestName, activated), reqsHere...), activated)
	}

	// Try candidates highest-version-first. bestCands is non-empty here, so the
	// loop runs at least once and always sets lastErr on failure.
	rest := withoutName(pending, bestName)
	var lastErr error
	for _, cand := range bestCands {
		activated[bestName] = &activation{cand: cand, requests: append([]req(nil), reqsHere...)}
		// Enqueue the candidate's own dependencies.
		next := append([]req(nil), rest...)
		for _, cd := range cand.Dependencies {
			next = append(next, req{dep: cd, requester: candLabel(bestName, cand)})
		}
		if err := r.solve(next, activated); err == nil {
			return nil
		} else {
			lastErr = err
		}
		delete(activated, bestName)
	}
	return lastErr
}

// candidates returns the index specs for name satisfying every requirement,
// sorted highest-version-first (release before prerelease unless a prerelease
// is explicitly requested).
func (r *resolver) candidates(name string, reqs []req) []IndexSpec {
	all := r.index.Versions(name)
	out := make([]IndexSpec, 0, len(all))
	for _, c := range all {
		if c.Version.Prerelease() && !r.allowPrerelease && !anyPrerelease(reqs) {
			continue
		}
		ok := true
		for _, rq := range reqs {
			if !rq.dep.Requirement.SatisfiedBy(c.Version) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Version.Compare(out[j].Version) > 0 })
	return out
}

// conflict builds a VersionConflict naming the gem and its (requirement ->
// requesters) map, with a Bundler-shaped message.
func (r *resolver) conflict(name string, reqs []req, activated map[string]*activation) *VersionConflict {
	requesters := map[string][]string{}
	for _, rq := range reqs {
		key := rq.dep.Requirement.String()
		requesters[key] = appendUnique(requesters[key], rq.requester)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Bundler could not find compatible versions for gem %q:\n", name)
	for _, key := range sortedKeys(requesters) {
		for _, who := range requesters[key] {
			fmt.Fprintf(&b, "  %s was resolved to depend on %q (%s)\n", who, name, key)
		}
	}
	return &VersionConflict{Name: name, Requesters: requesters, msg: strings.TrimRight(b.String(), "\n")}
}

// --- small helpers ---

func candLabel(name string, c IndexSpec) string { return name + " (" + c.Version.String() + ")" }

func groupByName(reqs []req) map[string][]req {
	m := map[string][]req{}
	for _, rq := range reqs {
		m[rq.dep.Name] = append(m[rq.dep.Name], rq)
	}
	return m
}

func reqsFor(name string, activated map[string]*activation) []req {
	if a, ok := activated[name]; ok {
		return a.requests
	}
	return nil
}

func withoutName(reqs []req, name string) []req {
	out := make([]req, 0, len(reqs))
	for _, rq := range reqs {
		if rq.dep.Name != name {
			out = append(out, rq)
		}
	}
	return out
}

func anyPrerelease(reqs []req) bool {
	for _, rq := range reqs {
		if rq.dep.Requirement.Prerelease() {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func appendUnique(ss []string, s string) []string {
	for _, x := range ss {
		if x == s {
			return ss
		}
	}
	return append(ss, s)
}
