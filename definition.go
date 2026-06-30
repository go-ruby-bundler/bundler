// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import "sort"

// Definition is the pure part of Bundler::Definition: the glue between the
// Gemfile's dependencies (supplied by the host after evaluating the Gemfile),
// the resolution source, the resolved SpecSet, and the lockfile production. The
// Gemfile evaluation, index fetch and install are all host-side seams.
type Definition struct {
	// Dependencies are the top-level Gemfile dependencies (DEPENDENCIES section).
	Dependencies []*Dependency
	// Platforms is the PLATFORMS section.
	Platforms []string
	// RubyVersion, when set, becomes the "RUBY VERSION" section.
	RubyVersion string
	// BundledWith is the Bundler version to record ("BUNDLED WITH").
	BundledWith string
	// Source is the source the resolved specs are attached to (a GEM remote in
	// the common case).
	Source *Source
}

// Resolve runs the resolver over the given index and returns a Resolution.
func (d *Definition) Resolve(index Index) (*Resolution, error) {
	return Resolve(d.Dependencies, index, d.Source)
}

// Lockfile turns a Resolution into a serializable Lockfile, grouping the
// resolved specs under their source. Specs are attached to d.Source (Resolve
// guarantees this), so a single source section is produced for the common GEM
// case; mixed-source resolutions group by the spec's own Source pointer.
func (d *Definition) Lockfile(res *Resolution) *Lockfile {
	lf := &Lockfile{
		Platforms:    append([]string(nil), d.Platforms...),
		Dependencies: append([]*Dependency(nil), d.Dependencies...),
		RubyVersion:  d.RubyVersion,
		BundledWith:  d.BundledWith,
	}

	// Group specs by source (pointer identity), preserving first-seen order.
	type group struct {
		src   *Source
		specs []*Spec
	}
	var groups []*group
	byPtr := map[*Source]*group{}
	for _, s := range res.Specs() {
		g, ok := byPtr[s.Source]
		if !ok {
			g = &group{src: s.Source}
			byPtr[s.Source] = g
			groups = append(groups, g)
		}
		g.specs = append(g.specs, s)
	}
	for _, g := range groups {
		sort.SliceStable(g.specs, func(i, j int) bool { return g.specs[i].fullName() < g.specs[j].fullName() })
		lf.Sources = append(lf.Sources, &LockSource{Source: g.src, Specs: g.specs})
	}
	return lf
}
