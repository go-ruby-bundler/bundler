// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import (
	"sort"
	"strings"

	"github.com/go-ruby-rubygems/rubygems"
)

// SpecDependency is one runtime dependency edge of a resolved spec: a gem name
// and its requirement. It mirrors a Gem::Dependency line nested under a spec in
// the lockfile's specs: tree.
type SpecDependency struct {
	Name        string
	Requirement *rubygems.Requirement
}

// sortKey is the Gem::Dependency#to_s of this dependency ("name (reqs)" with the
// requirement in its natural order, or "name (>= 0)" for none) — the key Bundler
// sorts a spec's dependency lines by.
func (d SpecDependency) sortKey() string {
	if d.Requirement == nil {
		return d.Name + " (>= 0)"
	}
	return d.Name + " (" + d.Requirement.String() + ")"
}

// lockLine renders the nested "      name (req)" line (6-space indent), matching
// Bundler's "    #{dep.to_lock}" where Gem::Dependency#to_lock already carries a
// 2-space lead: the requirement constraints are formatted "op v" and sorted in
// descending order, and a "none?" requirement (>= 0) is omitted.
func (d SpecDependency) lockLine() string {
	if d.Requirement == nil || d.Requirement.None() {
		return "      " + d.Name
	}
	parts := d.Requirement.AsList()
	sort.Sort(sort.Reverse(sort.StringSlice(parts)))
	return "      " + d.Name + " (" + strings.Join(parts, ", ") + ")"
}

// Spec is a resolved specification in the lockfile: a name, a version, an
// optional platform, the source it came from, and its runtime dependencies. It
// is the pure-data analogue of Bundler::LazySpecification.
type Spec struct {
	Name         string
	Version      *rubygems.Version
	Platform     string // "" means the "ruby" platform (Gem::Platform::RUBY)
	Source       *Source
	Dependencies []SpecDependency
}

// lockName renders the spec's "name (version)" / "name (version-platform)"
// identifier (Bundler::LazySpecification#lock_name / NameTuple#lock_name).
func (s *Spec) lockName() string {
	if s.Platform == "" || s.Platform == "ruby" {
		return s.Name + " (" + s.Version.String() + ")"
	}
	return s.Name + " (" + s.Version.String() + "-" + s.Platform + ")"
}

// fullName is the sort key Bundler uses to order specs in a source section
// (LazySpecification#full_name). It is "name-version" for the ruby platform and
// "name-version-platform" otherwise.
func (s *Spec) fullName() string {
	if s.Platform == "" || s.Platform == "ruby" {
		return s.Name + "-" + s.Version.String()
	}
	return s.Name + "-" + s.Version.String() + "-" + s.Platform
}

// toLock renders the spec block: the "    name (ver)" line followed by its
// runtime dependencies, sorted by Gem::Dependency#to_s (name first, so by name)
// and de-duplicated (Bundler::LazySpecification#to_lock). A spec never names the
// same dependency twice, so the to_s sort reduces to a stable name sort.
func (s *Spec) toLock() string {
	var b strings.Builder
	b.WriteString("    " + s.lockName() + "\n")

	deps := append([]SpecDependency(nil), s.Dependencies...)
	sort.SliceStable(deps, func(i, j int) bool { return deps[i].sortKey() < deps[j].sortKey() })
	var prev string
	for i, d := range deps {
		line := d.lockLine()
		if i > 0 && line == prev {
			continue
		}
		prev = line
		b.WriteString(line + "\n")
	}
	return b.String()
}
