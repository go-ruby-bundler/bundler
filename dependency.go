// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import (
	"github.com/go-ruby-rubygems/rubygems"
)

// Dependency is a Bundler::Dependency: a gem name plus a requirement, the
// bundler groups it belongs to, the platforms it applies to, and the source it
// is pinned to (if any). The groups/platforms drive `bundle install --without`
// and platform filtering; resolution itself uses name + requirement + source.
type Dependency struct {
	Name        string
	Requirement *rubygems.Requirement

	// Groups is the set of bundler groups (default [:default]); empty means the
	// default group.
	Groups []string
	// Platforms is the set of Gemfile platforms the dep applies to (e.g. "ruby",
	// "mri", "jruby"); empty means all.
	Platforms []string
	// Source, when non-nil, pins the dependency to a PATH or GIT source and
	// makes it print with a "!" suffix in DEPENDENCIES.
	Source *Source
}

// NewDependency builds a runtime Dependency from a name and constraint strings,
// using rubygems to parse the requirement. With no constraints the requirement
// defaults to ">= 0".
func NewDependency(name string, constraints ...string) (*Dependency, error) {
	req, err := rubygems.NewRequirement(constraints...)
	if err != nil {
		return nil, err
	}
	return &Dependency{Name: name, Requirement: req}, nil
}

// MustDependency is like NewDependency but panics on a malformed constraint.
func MustDependency(name string, constraints ...string) *Dependency {
	d, err := NewDependency(name, constraints...)
	if err != nil {
		panic(err)
	}
	return d
}

// pinned reports whether the dependency prints with a "!" marker: it is pinned
// to a non-GEM source (Bundler::Dependency#to_lock appends "!" when a source is
// set).
func (d *Dependency) pinned() bool { return d.Source != nil && d.Source.Type != GemSource }

// sortKey is Bundler::Dependency#to_s, the key the DEPENDENCIES section is
// sorted by: "name" for a default requirement, otherwise "name (reqs)".
func (d *Dependency) sortKey() string {
	if d.Requirement == nil || d.Requirement.None() {
		return d.Name
	}
	return d.Name + " (" + d.Requirement.String() + ")"
}

// lockLine renders the "  name (reqs)[!]" DEPENDENCIES line. A "none?" (>= 0)
// requirement is omitted; the requirement constraints are sorted descending
// (Bundler::Dependency#to_lock over Gem::Dependency#to_lock).
func (d *Dependency) lockLine() string {
	out := "  " + d.Name
	if d.Requirement != nil && !d.Requirement.None() {
		out += " (" + joinReqsDescending(d.Requirement) + ")"
	}
	if d.pinned() {
		out += "!"
	}
	return out
}
