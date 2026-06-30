// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import (
	"strings"
	"testing"
)

// buildIndex constructs the shared fixture index used by the resolver tests.
//
//	rake:   13.0.0, 13.4.2
//	a:      1.0.0 (-> c >= 1.0), 2.0.0 (-> c >= 2.0)
//	b:      1.0.0 (-> c < 2.0)
//	c:      1.0.0, 1.5.0, 2.0.0
//	d:      1.0.0 (-> a ~> 1.0)
//	pre:    1.0.0, 2.0.0.beta.1 (a prerelease)
func buildIndex(t *testing.T) MapIndex {
	t.Helper()
	idx := MapIndex{}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(idx.AddGem("rake", "13.0.0"))
	must(idx.AddGem("rake", "13.4.2"))
	must(idx.AddGem("a", "1.0.0", Dep("c", ">= 1.0")))
	must(idx.AddGem("a", "2.0.0", Dep("c", ">= 2.0")))
	must(idx.AddGem("b", "1.0.0", Dep("c", "< 2.0")))
	must(idx.AddGem("c", "1.0.0"))
	must(idx.AddGem("c", "1.5.0"))
	must(idx.AddGem("c", "2.0.0"))
	must(idx.AddGem("d", "1.0.0", Dep("a", "~> 1.0")))
	must(idx.AddGem("pre", "1.0.0"))
	must(idx.AddGem("pre", "2.0.0.beta.1"))
	return idx
}

func gemSource() *Source {
	return &Source{Type: GemSource, Remotes: []string{"https://rubygems.org/"}}
}

// TestResolveSimple resolves a single unconstrained dependency to the highest
// version.
func TestResolveSimple(t *testing.T) {
	idx := buildIndex(t)
	res, err := Resolve([]*Dependency{MustDependency("rake")}, idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Get("rake"); got == nil || got.Version.String() != "13.4.2" {
		t.Fatalf("rake = %v, want 13.4.2", got)
	}
}

// TestResolveConstrained honors a "~>" constraint.
func TestResolveConstrained(t *testing.T) {
	idx := buildIndex(t)
	res, err := Resolve([]*Dependency{MustDependency("rake", "~> 13.0.0")}, idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Get("rake").Version.String(); got != "13.0.0" {
		t.Fatalf("rake = %s, want 13.0.0", got)
	}
}

// TestResolveTransitive pulls in a transitive dependency.
func TestResolveTransitive(t *testing.T) {
	idx := buildIndex(t)
	res, err := Resolve([]*Dependency{MustDependency("d")}, idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	// d -> a ~> 1.0 -> a 1.0.0 -> c >= 1.0 -> c 2.0.0 (highest).
	if got := res.Get("a").Version.String(); got != "1.0.0" {
		t.Fatalf("a = %s, want 1.0.0", got)
	}
	if got := res.Get("c").Version.String(); got != "2.0.0" {
		t.Fatalf("c = %s, want 2.0.0", got)
	}
	if len(res.Specs()) != 3 {
		t.Fatalf("want 3 specs, got %d", len(res.Specs()))
	}
}

// TestResolveBacktrack forces backtracking: a wants c>=2.0 (so picks a 1.0.0 ok)
// but with b (c<2.0) present, a 2.0.0 (c>=2.0) conflicts; the resolver must pick
// a 1.0.0 + c 1.5.0 to satisfy both b (c<2.0) and a 1.0.0 (c>=1.0).
func TestResolveBacktrack(t *testing.T) {
	idx := buildIndex(t)
	res, err := Resolve([]*Dependency{MustDependency("a"), MustDependency("b")}, idx, gemSource())
	if err != nil {
		t.Fatalf("unexpected conflict: %v", err)
	}
	if got := res.Get("a").Version.String(); got != "1.0.0" {
		t.Fatalf("a = %s, want 1.0.0 (2.0.0 needs c>=2.0 which b forbids)", got)
	}
	c := res.Get("c").Version.String()
	if c != "1.5.0" {
		t.Fatalf("c = %s, want 1.5.0 (highest < 2.0 and >= 1.0)", c)
	}
}

// TestResolveConflict produces a VersionConflict with an explanatory message
// naming the gem and the requesters.
func TestResolveConflict(t *testing.T) {
	idx := MapIndex{}
	_ = idx.AddGem("x", "1.0.0", Dep("z", "~> 1.0"))
	_ = idx.AddGem("y", "1.0.0", Dep("z", "~> 2.0"))
	_ = idx.AddGem("z", "1.0.0")
	_ = idx.AddGem("z", "2.0.0")

	_, err := Resolve([]*Dependency{MustDependency("x"), MustDependency("y")}, idx, gemSource())
	if err == nil {
		t.Fatal("want VersionConflict")
	}
	vc, ok := err.(*VersionConflict)
	if !ok {
		t.Fatalf("want *VersionConflict, got %T", err)
	}
	if vc.Name != "z" {
		t.Fatalf("conflict gem = %q, want z", vc.Name)
	}
	if !strings.Contains(vc.Error(), "z") {
		t.Fatalf("message lacks gem name: %s", vc.Error())
	}
	// Both requesters appear.
	msg := vc.Error()
	if !strings.Contains(msg, "~> 1.0") || !strings.Contains(msg, "~> 2.0") {
		t.Fatalf("message lacks both requirements: %s", msg)
	}
}

// TestResolveMissingGem reports a conflict for a dependency the index does not
// know.
func TestResolveMissingGem(t *testing.T) {
	idx := buildIndex(t)
	_, err := Resolve([]*Dependency{MustDependency("nonexistent")}, idx, gemSource())
	if err == nil {
		t.Fatal("want conflict for missing gem")
	}
	if vc, ok := err.(*VersionConflict); !ok || vc.Name != "nonexistent" {
		t.Fatalf("want conflict on nonexistent, got %v", err)
	}
}

// TestResolveUnsatisfiableConstraint reports a conflict when no version of an
// existing gem matches.
func TestResolveUnsatisfiableConstraint(t *testing.T) {
	idx := buildIndex(t)
	_, err := Resolve([]*Dependency{MustDependency("rake", "~> 99.0")}, idx, gemSource())
	if err == nil {
		t.Fatal("want conflict")
	}
}

// TestResolvePrereleaseExcludedByDefault skips prereleases unless requested.
func TestResolvePrereleaseExcludedByDefault(t *testing.T) {
	idx := buildIndex(t)
	res, err := Resolve([]*Dependency{MustDependency("pre")}, idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Get("pre").Version.String(); got != "1.0.0" {
		t.Fatalf("pre = %s, want 1.0.0 (prerelease excluded)", got)
	}
}

// TestResolvePrereleaseRequested includes a prerelease when the requirement asks
// for one.
func TestResolvePrereleaseRequested(t *testing.T) {
	idx := buildIndex(t)
	res, err := Resolve([]*Dependency{MustDependency("pre", ">= 2.0.0.beta.1")}, idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Get("pre").Version.String(); got != "2.0.0.beta.1" {
		t.Fatalf("pre = %s, want 2.0.0.beta.1", got)
	}
}

// TestDefinitionLockfile drives the full Definition -> resolve -> lockfile path
// and checks the produced lockfile parses back to the same spec set.
func TestDefinitionLockfile(t *testing.T) {
	idx := buildIndex(t)
	def := &Definition{
		Dependencies: []*Dependency{MustDependency("d")},
		Platforms:    []string{"ruby"},
		BundledWith:  "2.6.9",
		Source:       gemSource(),
	}
	res, err := def.Resolve(idx)
	if err != nil {
		t.Fatal(err)
	}
	lf := def.Lockfile(res)
	out := lf.String()
	// Round-trips through the parser to the same spec names.
	reparsed, err := ParseLockfile(out)
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, out)
	}
	if len(reparsed.Sources[0].Specs) != 3 {
		t.Fatalf("want 3 specs in lock, got %d:\n%s", len(reparsed.Sources[0].Specs), out)
	}
	if !strings.Contains(out, "BUNDLED WITH\n   2.6.9\n") {
		t.Fatalf("missing bundled with:\n%s", out)
	}
}
