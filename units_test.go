// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import (
	"strings"
	"testing"

	"github.com/go-ruby-rubygems/rubygems"
)

// TestNewDependencyError exercises the malformed-constraint error path.
func TestNewDependencyError(t *testing.T) {
	if _, err := NewDependency("foo", "not a constraint"); err == nil {
		t.Fatal("want error for bad constraint")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("MustDependency should panic on bad constraint")
			}
		}()
		MustDependency("foo", "garbage!!")
	}()
}

// TestAddGemError exercises MapIndex.AddGem's bad-version path.
func TestAddGemError(t *testing.T) {
	idx := MapIndex{}
	if err := idx.AddGem("x", "not.a.version!"); err == nil {
		t.Fatal("want error for bad version")
	}
}

// TestBytes covers Lockfile.Bytes.
func TestBytes(t *testing.T) {
	lf := &Lockfile{Platforms: []string{"ruby"}, BundledWith: "2.6.9"}
	if len(lf.Bytes()) == 0 {
		t.Fatal("empty bytes")
	}
	if string(lf.Bytes()) != lf.String() {
		t.Fatal("Bytes != String")
	}
}

// TestParseErrorString covers ParseError.Error.
func TestParseErrorString(t *testing.T) {
	e := &ParseError{"boom"}
	if e.Error() != "boom" {
		t.Fatalf("got %q", e.Error())
	}
}

// TestGemRemoteEmpty covers the no-remote branch of GemRemote.
func TestGemRemoteEmpty(t *testing.T) {
	s := &Source{Type: GemSource}
	if s.GemRemote() != "" {
		t.Fatal("want empty remote")
	}
}

// TestSourcePinned covers Source.pinned for each type.
func TestSourcePinned(t *testing.T) {
	if (&Source{Type: GemSource}).pinned() {
		t.Fatal("GEM must not be pinned")
	}
	if !(&Source{Type: PathSource}).pinned() {
		t.Fatal("PATH must be pinned")
	}
	if !(&Source{Type: GitSource}).pinned() {
		t.Fatal("GIT must be pinned")
	}
}

// TestSpecPlatform covers the platform branches of lockName / fullName and the
// platform parse path.
func TestSpecPlatform(t *testing.T) {
	v := rubygems.MustVersion("1.0.0")
	s := &Spec{Name: "nokogiri", Version: v, Platform: "arm64-darwin"}
	if got := s.lockName(); got != "nokogiri (1.0.0-arm64-darwin)" {
		t.Fatalf("lockName: %s", got)
	}
	if got := s.fullName(); got != "nokogiri-1.0.0-arm64-darwin" {
		t.Fatalf("fullName: %s", got)
	}

	rubyS := &Spec{Name: "rake", Version: v, Platform: "ruby"}
	if got := rubyS.lockName(); got != "rake (1.0.0)" {
		t.Fatalf("ruby lockName: %s", got)
	}
	if got := rubyS.fullName(); got != "rake-1.0.0" {
		t.Fatalf("ruby fullName: %s", got)
	}
}

// TestSpecDepSortKeyNil covers the nil-requirement branch of sortKey.
func TestSpecDepSortKeyNil(t *testing.T) {
	d := SpecDependency{Name: "foo"}
	if got := d.sortKey(); got != "foo (>= 0)" {
		t.Fatalf("got %q", got)
	}
	if got := d.lockLine(); got != "      foo" {
		t.Fatalf("lockLine: %q", got)
	}
}

// TestDependencySortKeyNone covers the none-requirement branch of Dependency
// sortKey + lockLine.
func TestDependencySortKeyNone(t *testing.T) {
	d := &Dependency{Name: "foo"}
	if d.sortKey() != "foo" {
		t.Fatalf("sortKey: %q", d.sortKey())
	}
	if d.lockLine() != "  foo" {
		t.Fatalf("lockLine: %q", d.lockLine())
	}
}

// TestPlatformSpecRoundTrip parses and re-emits a lock with a platform-specific
// spec line, covering the platform branch in parseSpec and the platform branch
// in lockName/fullName.
func TestPlatformSpecRoundTrip(t *testing.T) {
	lock := "GEM\n" +
		"  remote: https://rubygems.org/\n" +
		"  specs:\n" +
		"    nokogiri (1.16.0)\n" +
		"    nokogiri (1.16.0-arm64-darwin)\n" +
		"\nPLATFORMS\n  arm64-darwin\n  ruby\n" +
		"\nDEPENDENCIES\n  nokogiri\n" +
		"\nBUNDLED WITH\n   2.6.9\n"
	lf, err := ParseLockfile(lock)
	if err != nil {
		t.Fatal(err)
	}
	if got := lf.String(); got != lock {
		t.Fatalf("round-trip:\n--- want ---\n%q\n--- got ---\n%q", lock, got)
	}
}

// TestSubmodulesAndGlobGit covers the submodules and non-default glob git
// branches in the source header.
func TestSubmodulesAndGlobGit(t *testing.T) {
	lock := "GIT\n" +
		"  remote: https://example.com/x.git\n" +
		"  revision: abc123\n" +
		"  tag: v1.0\n" +
		"  submodules: true\n" +
		"  glob: lib/*.gemspec\n" +
		"  specs:\n" +
		"    x (1.0.0)\n" +
		"\nPLATFORMS\n  ruby\n" +
		"\nDEPENDENCIES\n  x!\n" +
		"\nBUNDLED WITH\n   2.6.9\n"
	lf, err := ParseLockfile(lock)
	if err != nil {
		t.Fatal(err)
	}
	g := lf.Sources[0].Source
	if g.Tag != "v1.0" || g.Submodules != "true" || g.Glob != "lib/*.gemspec" {
		t.Fatalf("git opts: %+v", g)
	}
	if got := lf.String(); got != lock {
		t.Fatalf("round-trip:\n%q\nvs\n%q", lock, got)
	}
}

// TestPinnedDepNoMatchingSpec covers the parseDependency branch where a pinned
// "!" dependency has no matching spec (a sentinel PATH source is attached).
func TestPinnedDepNoMatchingSpec(t *testing.T) {
	lock := "GEM\n  remote: https://rubygems.org/\n  specs:\n" +
		"\nPLATFORMS\n  ruby\n" +
		"\nDEPENDENCIES\n  orphan!\n" +
		"\nBUNDLED WITH\n   2.6.9\n"
	lf, err := ParseLockfile(lock)
	if err != nil {
		t.Fatal(err)
	}
	if !lf.Dependencies[0].pinned() {
		t.Fatal("orphan should be pinned")
	}
	if got := lf.String(); got != lock {
		t.Fatalf("round-trip:\n%q\nvs\n%q", lock, got)
	}
}

// TestBundlerPinnedNotMarked covers the "bundler" exception in parseDependency:
// a "bundler!" line does not attach a source.
func TestBundlerPinnedNotMarked(t *testing.T) {
	lf, err := ParseLockfile("DEPENDENCIES\n  bundler!\n")
	if err != nil {
		t.Fatal(err)
	}
	if lf.Dependencies[0].pinned() {
		t.Fatal("bundler dep should not be marked pinned")
	}
}

// TestUnknownSectionResetsMode covers the unknown-top-level-header branch.
func TestUnknownSectionResetsMode(t *testing.T) {
	lock := "GEM\n  remote: https://rubygems.org/\n  specs:\n    rake (13.0.0)\n" +
		"\nWAT\n  ignored stuff\n" +
		"\nDEPENDENCIES\n  rake\n"
	lf, err := ParseLockfile(lock)
	if err != nil {
		t.Fatal(err)
	}
	if len(lf.Sources[0].Specs) != 1 {
		t.Fatalf("specs: %d", len(lf.Sources[0].Specs))
	}
}

// TestBadVersionInSpec covers the version-parse error path of parseSpec. RubyGems
// rejects a version starting with a letter.
func TestBadVersionInSpec(t *testing.T) {
	_, err := ParseLockfile("GEM\n  remote: r\n  specs:\n    foo (bad!)\n")
	if err == nil {
		t.Fatal("want version parse error")
	}
}

// TestBadRequirementInNestedDep covers the nested-dependency requirement error
// path.
func TestBadRequirementInNestedDep(t *testing.T) {
	_, err := ParseLockfile("GEM\n  remote: r\n  specs:\n    foo (1.0.0)\n      bar (>>> 1)\n")
	if err == nil {
		t.Fatal("want nested requirement error")
	}
}

// TestBadRequirementInDependency covers the DEPENDENCIES requirement error path.
func TestBadRequirementInDependency(t *testing.T) {
	_, err := ParseLockfile("DEPENDENCIES\n  foo (>>> 1)\n")
	if err == nil {
		t.Fatal("want dependency requirement error")
	}
}

// TestNestedDepBeforeSpec covers the parseSpec branch where a 6-space line
// appears with no current spec (defensive nil guard).
func TestNestedDepBeforeSpec(t *testing.T) {
	// A 6-space dep line under specs: with no preceding 4-space spec line.
	lf, err := ParseLockfile("GEM\n  remote: r\n  specs:\n      orphan (~> 1)\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(lf.Sources[0].Specs) != 0 {
		t.Fatal("no spec should have been created")
	}
}

// TestParsePlatformVersion covers the RUBY VERSION + multi-line bundled-with
// validation (a non-version BUNDLED WITH line is ignored).
func TestBundledWithNonVersionIgnored(t *testing.T) {
	lf, err := ParseLockfile("BUNDLED WITH\n   notaversion!!\n")
	if err != nil {
		t.Fatal(err)
	}
	if lf.BundledWith != "" {
		t.Fatalf("want empty bundled-with, got %q", lf.BundledWith)
	}
}

// TestResolveDiamond covers the "already activated" path of solve: two gems
// depend on the same shared gem, so the shared gem is re-encountered after
// activation and must satisfy both.
func TestResolveDiamond(t *testing.T) {
	idx := MapIndex{}
	_ = idx.AddGem("p", "1.0.0", Dep("shared", ">= 1.0"))
	_ = idx.AddGem("q", "1.0.0", Dep("shared", "< 2.0"))
	_ = idx.AddGem("shared", "1.0.0")
	_ = idx.AddGem("shared", "2.0.0")

	res, err := Resolve([]*Dependency{MustDependency("p"), MustDependency("q")}, idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	// shared must be 1.0.0 (the only version satisfying >= 1.0 AND < 2.0).
	if got := res.Get("shared").Version.String(); got != "1.0.0" {
		t.Fatalf("shared = %s, want 1.0.0", got)
	}
}

// TestResolveActivatedThenSatisfied hits the "already activated and satisfied"
// branch of solve: a forces c = 2.0 (single candidate, activated first) and also
// pulls in b, whose c >= 1.0 is then checked against the already-activated c.
func TestResolveActivatedThenSatisfied(t *testing.T) {
	idx := MapIndex{}
	_ = idx.AddGem("a", "1.0.0", Dep("c", "= 2.0"), Dep("b", ">= 0"))
	_ = idx.AddGem("b", "1.0.0", Dep("c", ">= 1.0"))
	_ = idx.AddGem("c", "1.0.0")
	_ = idx.AddGem("c", "2.0.0")
	res, err := Resolve([]*Dependency{MustDependency("a")}, idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Get("c").Version.String(); got != "2.0.0" {
		t.Fatalf("c = %s, want 2.0.0", got)
	}
}

// TestResolveActivatedThenConflict hits the conflict sub-branch of the "already
// activated" path: a forces c = 2.0 (the only candidate), then b requires c < 2.0
// which the already-activated c violates, with no alternative c to backtrack to.
func TestResolveActivatedThenConflict(t *testing.T) {
	idx := MapIndex{}
	_ = idx.AddGem("a", "1.0.0", Dep("c", "= 2.0"), Dep("b", ">= 0"))
	_ = idx.AddGem("b", "1.0.0", Dep("c", "< 2.0"))
	_ = idx.AddGem("c", "2.0.0")
	_, err := Resolve([]*Dependency{MustDependency("a")}, idx, gemSource())
	if err == nil {
		t.Fatal("want conflict")
	}
	if _, ok := err.(*VersionConflict); !ok {
		t.Fatalf("want VersionConflict, got %T", err)
	}
}

// TestResolveBacktrackThroughActivated forces backtracking back through an
// already-activated package: a has two versions; the first leads to a dead end
// via an already-activated shared gem, so the resolver must undo it and try the
// next a.
func TestResolveBacktrackThroughActivated(t *testing.T) {
	idx := MapIndex{}
	// root -> m, a. m pins c = 1.0 (single candidate, activated early).
	// a 2.0.0 -> c >= 2.0 (conflicts with activated c=1.0 -> backtrack a),
	// a 1.0.0 -> c >= 1.0 (ok with c=1.0).
	_ = idx.AddGem("m", "1.0.0", Dep("c", "= 1.0"))
	_ = idx.AddGem("a", "1.0.0", Dep("c", ">= 1.0"))
	_ = idx.AddGem("a", "2.0.0", Dep("c", ">= 2.0"))
	_ = idx.AddGem("c", "1.0.0")
	_ = idx.AddGem("c", "2.0.0")
	res, err := Resolve([]*Dependency{MustDependency("m"), MustDependency("a")}, idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Get("a").Version.String(); got != "1.0.0" {
		t.Fatalf("a = %s, want 1.0.0 (2.0.0 conflicts with c=1.0)", got)
	}
	if got := res.Get("c").Version.String(); got != "1.0.0" {
		t.Fatalf("c = %s, want 1.0.0", got)
	}
}

// TestResolveEmpty covers the no-dependencies success path.
func TestResolveEmpty(t *testing.T) {
	res, err := Resolve(nil, MapIndex{}, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Specs()) != 0 {
		t.Fatal("want empty resolution")
	}
}

// TestResolveNilRequirement covers the d.Requirement == nil default branch in
// Resolve.
func TestResolveNilRequirement(t *testing.T) {
	idx := MapIndex{}
	_ = idx.AddGem("foo", "1.0.0")
	res, err := Resolve([]*Dependency{{Name: "foo"}}, idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	if res.Get("foo").Version.String() != "1.0.0" {
		t.Fatal("foo not resolved")
	}
}

// TestConflictMessageShape checks the conflict message names the gem and lists
// each requirement with its requester.
func TestConflictMessageShape(t *testing.T) {
	idx := MapIndex{}
	_ = idx.AddGem("x", "1.0.0", Dep("z", "= 1.0"))
	_ = idx.AddGem("y", "1.0.0", Dep("z", "= 2.0"))
	_ = idx.AddGem("z", "1.0.0")
	_ = idx.AddGem("z", "2.0.0")
	_, err := Resolve([]*Dependency{MustDependency("x"), MustDependency("y")}, idx, gemSource())
	vc := err.(*VersionConflict)
	msg := vc.Error()
	if !strings.Contains(msg, "gem \"z\"") {
		t.Fatalf("message: %s", msg)
	}
	if len(vc.Requesters) == 0 {
		t.Fatal("no requesters recorded")
	}
}

// TestSerializeBundlerSkipAndDedup covers the bundler-skip branch when emitting
// specs and the duplicate-name dedup in the DEPENDENCIES section.
func TestSerializeBundlerSkipAndDedup(t *testing.T) {
	src := &Source{Type: GemSource, Remotes: []string{"https://rubygems.org/"}}
	lf := &Lockfile{
		Sources: []*LockSource{{
			Source: src,
			Specs: []*Spec{
				{Name: "rake", Version: rubygems.MustVersion("13.0.0"), Source: src},
				{Name: "bundler", Version: rubygems.MustVersion("2.6.9"), Source: src},
			},
		}},
		Platforms: []string{"ruby"},
		Dependencies: []*Dependency{
			MustDependency("rake"),
			MustDependency("rake"), // duplicate name -> deduped on emit
		},
		BundledWith: "2.6.9",
	}
	out := lf.String()
	if strings.Contains(out, "bundler (2.6.9)") {
		t.Fatalf("bundler spec should be skipped:\n%s", out)
	}
	if strings.Count(out, "  rake\n") != 1 {
		t.Fatalf("rake dependency should appear once:\n%s", out)
	}
}

// TestSerializePathGlob covers the non-default PATH glob branch of the header.
func TestSerializePathGlob(t *testing.T) {
	src := &Source{Type: PathSource, Remotes: []string{"vendor/x"}, Glob: "lib/*.gemspec"}
	lf := &Lockfile{
		Sources:      []*LockSource{{Source: src, Specs: []*Spec{{Name: "x", Version: rubygems.MustVersion("1.0.0"), Source: src}}}},
		Platforms:    []string{"ruby"},
		Dependencies: []*Dependency{{Name: "x", Requirement: rubygems.DefaultRequirement(), Source: src}},
		BundledWith:  "2.6.9",
	}
	out := lf.String()
	if !strings.Contains(out, "  glob: lib/*.gemspec\n") {
		t.Fatalf("missing path glob:\n%s", out)
	}
}

// TestSerializeDuplicateSpecDep covers the duplicate-dependency-line dedup in
// Spec.toLock.
func TestSerializeDuplicateSpecDep(t *testing.T) {
	src := &Source{Type: GemSource, Remotes: []string{"r"}}
	s := &Spec{
		Name:    "foo",
		Version: rubygems.MustVersion("1.0.0"),
		Source:  src,
		Dependencies: []SpecDependency{
			{Name: "bar", Requirement: rubygems.MustRequirement("~> 1.0")},
			{Name: "bar", Requirement: rubygems.MustRequirement("~> 1.0")}, // dup
		},
	}
	out := s.toLock()
	if strings.Count(out, "bar (~> 1.0)") != 1 {
		t.Fatalf("duplicate dep not deduped:\n%s", out)
	}
}

// TestResolvePrereleaseViaTransitiveRequest covers anyPrerelease's true branch:
// a prerelease candidate is allowed because a (transitive) requirement targets a
// prerelease version explicitly.
func TestResolvePrereleaseViaTransitiveRequest(t *testing.T) {
	idx := MapIndex{}
	_ = idx.AddGem("top", "1.0.0", Dep("dep", ">= 2.0.0.beta.1"))
	_ = idx.AddGem("dep", "1.0.0")
	_ = idx.AddGem("dep", "2.0.0.beta.1")
	res, err := Resolve([]*Dependency{MustDependency("top")}, idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Get("dep").Version.String(); got != "2.0.0.beta.1" {
		t.Fatalf("dep = %s, want 2.0.0.beta.1", got)
	}
}

// TestResolveUndoAfterActivatedSatisfied covers the undo path of the
// already-activated branch: c is activated and satisfies a later requirement,
// but a still-deeper dependency then fails, forcing the appended requests to be
// rolled back as the search backtracks to a different candidate.
func TestResolveUndoAfterActivatedSatisfied(t *testing.T) {
	idx := MapIndex{}
	// root -> a, m. m pins c = 1.0 (single candidate). a has two versions:
	//   a 2.0.0 -> c >= 1.0 (satisfied by activated c=1.0) AND k >= 5.0 (no such k
	//             -> dead end after the already-activated c is accepted -> undo),
	//   a 1.0.0 -> c >= 1.0 (ok), no impossible k.
	_ = idx.AddGem("m", "1.0.0", Dep("c", "= 1.0"))
	_ = idx.AddGem("a", "2.0.0", Dep("c", ">= 1.0"), Dep("k", ">= 5.0"))
	_ = idx.AddGem("a", "1.0.0", Dep("c", ">= 1.0"))
	_ = idx.AddGem("c", "1.0.0")
	_ = idx.AddGem("k", "1.0.0")
	res, err := Resolve([]*Dependency{MustDependency("m"), MustDependency("a")}, idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Get("a").Version.String(); got != "1.0.0" {
		t.Fatalf("a = %s, want 1.0.0 (2.0.0 needs k>=5.0 which is absent)", got)
	}
}

// TestResolveActivatedUndoOnSiblingFailure covers the undo of appended requests
// on the already-activated branch (resolver.go a.requests rollback). c is
// activated first (single candidate, most-constrained). a then arrives as a root
// dep alongside a sibling p; a's only version re-requests c (already activated,
// satisfied -> requests appended) but its sibling q dependency is unsatisfiable,
// so the recursion under the already-activated c fails and the appended requests
// must be rolled back before the conflict propagates.
func TestResolveActivatedUndoOnSiblingFailure(t *testing.T) {
	idx := MapIndex{}
	// c: single pinned candidate, activated first by m.
	_ = idx.AddGem("m", "1.0.0", Dep("c", "= 1.0"))
	_ = idx.AddGem("c", "1.0.0")
	// a (single version) -> c (already-activated, satisfied, 1 candidate) and w.
	// w has exactly one candidate, so c and w tie at 1 candidate; c sorts first
	// (c < w), so the already-activated-c branch runs: it appends c's request and
	// recurses into the rest (w). w's only version needs an absent gem z, so the
	// recursion fails and the appended c request is rolled back (undo path).
	_ = idx.AddGem("a", "1.0.0", Dep("c", ">= 1.0"), Dep("w", ">= 1.0"))
	_ = idx.AddGem("w", "1.0.0", Dep("z", ">= 1.0")) // z is absent
	_, err := Resolve([]*Dependency{MustDependency("m"), MustDependency("a")}, idx, gemSource())
	if err == nil {
		t.Fatal("want conflict on absent gem z")
	}
	if vc, ok := err.(*VersionConflict); !ok || vc.Name != "z" {
		t.Fatalf("want VersionConflict on z, got %v", err)
	}
}

// TestParseDependencyWrongIndent covers the DEPENDENCIES-line indent guard in
// parser.go: a line indented other than 2 spaces under DEPENDENCIES is ignored
// (Bundler only reads the 2-space dependency lines).
func TestParseDependencyWrongIndent(t *testing.T) {
	// The "    rake" line (4 spaces) under DEPENDENCIES is skipped; only the
	// 2-space "  rails" line becomes a dependency.
	lock := "GEM\n  remote: https://rubygems.org/\n  specs:\n    rails (7.0.0)\n\n" +
		"PLATFORMS\n  ruby\n\nDEPENDENCIES\n    rake\n  rails\n\nBUNDLED WITH\n   2.6.9\n"
	lf, err := ParseLockfile(lock)
	if err != nil {
		t.Fatal(err)
	}
	if len(lf.Dependencies) != 1 || lf.Dependencies[0].Name != "rails" {
		t.Fatalf("want only [rails], got %+v", lf.Dependencies)
	}
}

// TestDependencyMergeViaAppendUnique indirectly covers appendUnique's
// already-present branch: a gem required twice by the same requester collapses
// to one entry.
func TestAppendUniqueDuplicate(t *testing.T) {
	got := appendUnique([]string{"a"}, "a")
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	got = appendUnique(got, "b")
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
}
