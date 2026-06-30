// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import (
	"os"
	"path/filepath"
	"testing"
)

// readFixture loads a testdata lockfile, normalizing CRLF to LF so the
// byte-for-byte round-trip assertion holds on a Windows checkout too (the
// .gitattributes pins LF, but normalize defensively).
func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	s := string(b)
	// Defensive CRLF -> LF (Windows autocrlf safety).
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// TestRoundTrip parses each real Bundler lockfile fixture and re-emits it,
// asserting byte-for-byte equality — the headline lockfile-codec parity claim.
func TestRoundTrip(t *testing.T) {
	fixtures := []string{
		"gem.lock", "path.lock", "git_branch.lock", "git_ref.lock",
		"ruby_version.lock", "multi_remote.lock",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			want := readFixture(t, name)
			lf, err := ParseLockfile(want)
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			got := lf.String()
			if got != want {
				t.Fatalf("round-trip mismatch for %s:\n--- want ---\n%q\n--- got ---\n%q", name, want, got)
			}
		})
	}
}

// TestParseGemSection checks the structural parse of the GEM fixture: the
// remote, the spec set, the nested dependencies, the platforms and the
// top-level dependencies.
func TestParseGemSection(t *testing.T) {
	lf, err := ParseLockfile(readFixture(t, "gem.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if len(lf.Sources) != 1 {
		t.Fatalf("want 1 source, got %d", len(lf.Sources))
	}
	src := lf.Sources[0]
	if src.Source.Type != GemSource || src.Source.GemRemote() != "https://rubygems.org/" {
		t.Fatalf("bad source: %+v", src.Source)
	}
	// minitest has two nested deps (drb, prism).
	var minitest *Spec
	for _, s := range src.Specs {
		if s.Name == "minitest" {
			minitest = s
		}
	}
	if minitest == nil {
		t.Fatal("minitest spec not found")
	}
	if got := minitest.Version.String(); got != "6.0.6" {
		t.Fatalf("minitest version: %s", got)
	}
	if len(minitest.Dependencies) != 2 {
		t.Fatalf("minitest deps: %d", len(minitest.Dependencies))
	}
	if lf.BundledWith != "2.6.9" {
		t.Fatalf("bundled with: %q", lf.BundledWith)
	}
	if len(lf.Dependencies) != 3 {
		t.Fatalf("deps: %d", len(lf.Dependencies))
	}
}

// TestParsePathPinned verifies the PATH source and the "!" pinned dependency
// round-trip with the source attached.
func TestParsePathPinned(t *testing.T) {
	lf, err := ParseLockfile(readFixture(t, "path.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if lf.Sources[0].Source.Type != PathSource {
		t.Fatal("first source not PATH")
	}
	var mygem *Dependency
	for _, d := range lf.Dependencies {
		if d.Name == "mygem" {
			mygem = d
		}
	}
	if mygem == nil || !mygem.pinned() {
		t.Fatalf("mygem not pinned: %+v", mygem)
	}
}

// TestParseGitOptions verifies the GIT source revision/branch/ref/glob fields.
func TestParseGitOptions(t *testing.T) {
	lf, err := ParseLockfile(readFixture(t, "git_branch.lock"))
	if err != nil {
		t.Fatal(err)
	}
	g := lf.Sources[0].Source
	if g.Type != GitSource {
		t.Fatal("not git")
	}
	if g.Revision != "474500ab2eec41ef28068e18e30f84fa412a098f" {
		t.Fatalf("revision: %q", g.Revision)
	}
	if g.Branch != "master" {
		t.Fatalf("branch: %q", g.Branch)
	}
	if g.Glob != "*.gemspec" {
		t.Fatalf("glob: %q", g.Glob)
	}

	ref, err := ParseLockfile(readFixture(t, "git_ref.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if ref.Sources[0].Source.Ref == "" {
		t.Fatal("ref not parsed")
	}
}

// TestParseMultiRemote verifies the two-remote GEM section reverses correctly so
// it re-emits in the original order.
func TestParseMultiRemote(t *testing.T) {
	lf, err := ParseLockfile(readFixture(t, "multi_remote.lock"))
	if err != nil {
		t.Fatal(err)
	}
	rems := lf.Sources[0].Source.Remotes
	if len(rems) != 2 {
		t.Fatalf("want 2 remotes, got %d", len(rems))
	}
}

// TestMergeConflict rejects a lockfile with merge-conflict markers.
func TestMergeConflict(t *testing.T) {
	_, err := ParseLockfile("GEM\n<<<<<<< HEAD\n")
	if err == nil {
		t.Fatal("want merge-conflict error")
	}
	if _, ok := err.(*ParseError); !ok {
		t.Fatalf("want *ParseError, got %T", err)
	}
}
