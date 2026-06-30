// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import (
	"sort"
	"strings"

	"github.com/go-ruby-rubygems/rubygems"
)

// joinReqsDescending formats a requirement's constraints as "op v" and joins
// them in descending sort order, matching Gem::Dependency#to_lock
// (reqs.sort.reverse).
func joinReqsDescending(r *rubygems.Requirement) string {
	parts := r.AsList()
	sort.Sort(sort.Reverse(sort.StringSlice(parts)))
	return strings.Join(parts, ", ")
}

// Lockfile is the parsed/parseable model of a Gemfile.lock: the source sections
// with their specs, the platforms, the top-level dependencies, an optional
// locked ruby version, and the bundler version. Serializing it reproduces the
// original bytes.
type Lockfile struct {
	// Sources holds the GEM/PATH/GIT sections in lock order, each carrying the
	// specs that resolved against it.
	Sources []*LockSource
	// Platforms is the PLATFORMS section (sorted on emit).
	Platforms []string
	// Dependencies is the DEPENDENCIES section (the top-level Gemfile deps).
	Dependencies []*Dependency
	// RubyVersion is the "RUBY VERSION" section value ("" when absent).
	RubyVersion string
	// BundledWith is the "BUNDLED WITH" version ("" when absent).
	BundledWith string
}

// LockSource pairs a Source header with the specs locked under it.
type LockSource struct {
	Source *Source
	Specs  []*Spec
}

// String serializes the lockfile to its canonical bytes. It matches Bundler's
// LockfileGenerator: sources (each header + its specs sorted by full_name,
// bundler itself skipped), then PLATFORMS, DEPENDENCIES, RUBY VERSION and
// BUNDLED WITH, separated by single blank lines.
func (l *Lockfile) String() string {
	var b strings.Builder

	for i, ls := range l.Sources {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(ls.Source.header())
		specs := append([]*Spec(nil), ls.Specs...)
		sort.SliceStable(specs, func(a, c int) bool { return specs[a].fullName() < specs[c].fullName() })
		for _, s := range specs {
			if s.Name == "bundler" {
				continue
			}
			b.WriteString(s.toLock())
		}
	}

	// PLATFORMS
	b.WriteString("\nPLATFORMS\n")
	plats := append([]string(nil), l.Platforms...)
	sort.Strings(plats)
	for _, p := range plats {
		b.WriteString("  " + p + "\n")
	}

	// DEPENDENCIES — sorted by Dependency#to_s, de-duplicated by name.
	b.WriteString("\nDEPENDENCIES\n")
	deps := append([]*Dependency(nil), l.Dependencies...)
	sort.SliceStable(deps, func(a, c int) bool { return deps[a].sortKey() < deps[c].sortKey() })
	handled := map[string]bool{}
	for _, d := range deps {
		if handled[d.Name] {
			continue
		}
		handled[d.Name] = true
		b.WriteString(d.lockLine() + "\n")
	}

	// RUBY VERSION (optional)
	if l.RubyVersion != "" {
		b.WriteString("\nRUBY VERSION\n   " + l.RubyVersion + "\n")
	}

	// BUNDLED WITH (optional)
	if l.BundledWith != "" {
		b.WriteString("\nBUNDLED WITH\n   " + l.BundledWith + "\n")
	}

	return b.String()
}

// Bytes is String as a byte slice, for writing to a file (a host-side seam).
func (l *Lockfile) Bytes() []byte { return []byte(l.String()) }
