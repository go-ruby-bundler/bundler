// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-ruby-rubygems/rubygems"
)

// The MRI oracle. These tests build a local, offline gem repository (a set of
// packed .gem files plus a generated index), then drive the real `bundle`
// binary against it with `bundle lock`, and assert that:
//
//   - our resolver activates the exact same name->version set as Bundler, and
//   - our lockfile serializer reproduces Bundler's Gemfile.lock byte-for-byte
//     (modulo the host-specific PLATFORMS line, which is normalized).
//
// They skip themselves when `ruby`/`bundle` are unavailable (the Windows lane
// and the cross-arch qemu lanes), so the deterministic ruby-free suite alone
// keeps coverage at 100% there. No network: the gem source is a file:// URL.

// oracleGem describes one gem to pack into the local repo.
type oracleGem struct {
	name, version string
	deps          [][2]string // {name, constraint}
}

// oracleCorpus is the gem set the oracle resolves over. It mirrors the
// in-memory fixture index used by the deterministic resolver tests so the two
// suites agree by construction.
var oracleCorpus = []oracleGem{
	{name: "rake", version: "13.0.0"},
	{name: "rake", version: "13.4.2"},
	{name: "a", version: "1.0.0", deps: [][2]string{{"c", ">= 1.0"}}},
	{name: "a", version: "2.0.0", deps: [][2]string{{"c", ">= 2.0"}}},
	{name: "b", version: "1.0.0", deps: [][2]string{{"c", "< 2.0"}}},
	{name: "c", version: "1.0.0"},
	{name: "c", version: "1.5.0"},
	{name: "c", version: "2.0.0"},
	{name: "d", version: "1.0.0", deps: [][2]string{{"a", "~> 1.0"}}},
}

// oracleBundlerVersion is the Bundler the byte-for-byte serialization is matched
// against (the lockfile format target). A newer default Bundler (4.x) adds a
// CHECKSUMS section, so the bytes test pins this exact version via the
// `bundle _VERSION_ ...` selector; it skips if that version is not installed.
const oracleBundlerVersion = "2.6.9"

func bundleBin(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("oracle skipped on Windows")
	}
	p, err := exec.LookPath("bundle")
	if err != nil {
		t.Skip("bundle not on PATH; skipping Bundler oracle")
	}
	if _, err := exec.LookPath("gem"); err != nil {
		t.Skip("gem not on PATH; skipping Bundler oracle")
	}
	return p
}

// pinnedBundle reports whether the pinned Bundler version is installed; the
// byte-for-byte lockfile test needs it. Resolution-set parity does not (any
// Bundler resolves the same set), so only the bytes test gates on this.
func pinnedBundle(t *testing.T, bundle string) bool {
	cmd := exec.Command("gem", "list", "-e", "bundler")
	out, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(string(out), oracleBundlerVersion)
}

// oracleIndex builds the MapIndex matching oracleCorpus, for our own resolver.
func oracleIndex(t *testing.T) MapIndex {
	t.Helper()
	idx := MapIndex{}
	for _, g := range oracleCorpus {
		var deps []SpecDependency
		for _, d := range g.deps {
			deps = append(deps, Dep(d[0], d[1]))
		}
		if err := idx.AddGem(g.name, g.version, deps...); err != nil {
			t.Fatal(err)
		}
	}
	return idx
}

// buildGemRepo packs oracleCorpus into dir/gems and generates the index. It is a
// no-op-skip on any gem-build failure (e.g. a sandbox without a writable HOME).
func buildGemRepo(t *testing.T, dir string) {
	t.Helper()
	gemsDir := filepath.Join(dir, "gems")
	if err := os.MkdirAll(gemsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, g := range oracleCorpus {
		work := t.TempDir()
		var sb strings.Builder
		fmt.Fprintf(&sb, "Gem::Specification.new do |s|\n")
		fmt.Fprintf(&sb, "  s.name=%q\n  s.version=%q\n  s.summary='x'\n  s.authors=['x']\n  s.files=[]\n", g.name, g.version)
		for _, d := range g.deps {
			fmt.Fprintf(&sb, "  s.add_dependency %q, %q\n", d[0], d[1])
		}
		fmt.Fprintf(&sb, "end\n")
		spec := filepath.Join(work, g.name+".gemspec")
		if err := os.WriteFile(spec, []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(gemsDir, fmt.Sprintf("%s-%s.gem", g.name, g.version))
		cmd := exec.Command("gem", "build", g.name+".gemspec", "-o", out)
		cmd.Dir = work
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("gem build failed (sandbox?): %v\n%s", err, b)
		}
	}
	cmd := exec.Command("gem", "generate_index", "--directory", dir)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("gem generate_index failed: %v\n%s", err, b)
	}
}

// bundleLock writes a Gemfile with the given gem lines against the local repo,
// runs `bundle lock`, and returns the resulting Gemfile.lock (or "" + the error
// output on a resolution failure).
func bundleLock(t *testing.T, bundle, repo string, gemLines []string, versionSelector string) (lock string, failed bool, errOut string) {
	t.Helper()
	work := t.TempDir()
	var gf strings.Builder
	fmt.Fprintf(&gf, "source \"file://%s\"\n", repo)
	for _, l := range gemLines {
		gf.WriteString(l + "\n")
	}
	gemfile := filepath.Join(work, "Gemfile")
	if err := os.WriteFile(gemfile, []byte(gf.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{}
	if versionSelector != "" {
		args = append(args, "_"+versionSelector+"_")
	}
	args = append(args, "lock")
	cmd := exec.Command(bundle, args...)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "BUNDLE_GEMFILE="+gemfile)
	out, _ := cmd.CombinedOutput()
	lockPath := filepath.Join(work, "Gemfile.lock")
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return "", true, string(out)
	}
	return string(b), false, string(out)
}

// resolvedSet extracts the name->version map from a Gemfile.lock's GEM specs.
func resolvedSet(t *testing.T, lock string) map[string]string {
	t.Helper()
	lf, err := ParseLockfile(lock)
	if err != nil {
		t.Fatalf("parse oracle lock: %v\n%s", err, lock)
	}
	m := map[string]string{}
	for _, ls := range lf.Sources {
		for _, s := range ls.Specs {
			m[s.Name] = s.Version.String()
		}
	}
	return m
}

// ourSet runs our resolver and returns the name->version map.
func ourSet(t *testing.T, gemDeps []*Dependency, idx Index) (map[string]string, error) {
	res, err := Resolve(gemDeps, idx, gemSource())
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, s := range res.Specs() {
		m[s.Name] = s.Version.String()
	}
	return m, nil
}

func sameSet(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func dumpSet(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s ", k, m[k])
	}
	return b.String()
}

// TestOracleResolutionParity is the headline resolver parity claim: for each
// scenario, our resolver's activated set equals Bundler's.
func TestOracleResolutionParity(t *testing.T) {
	bundle := bundleBin(t)
	repo := t.TempDir()
	buildGemRepo(t, repo)
	idx := oracleIndex(t)

	cases := []struct {
		name     string
		gemLines []string
		deps     []*Dependency
	}{
		{"simple", []string{`gem "rake"`}, []*Dependency{MustDependency("rake")}},
		{"constrained", []string{`gem "rake", "~> 13.0.0"`}, []*Dependency{MustDependency("rake", "~> 13.0.0")}},
		{"transitive", []string{`gem "d"`}, []*Dependency{MustDependency("d")}},
		{"backtrack", []string{`gem "a"`, `gem "b"`}, []*Dependency{MustDependency("a"), MustDependency("b")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lock, failed, errOut := bundleLock(t, bundle, repo, c.gemLines, "")
			if failed {
				t.Fatalf("bundler unexpectedly failed: %s", errOut)
			}
			want := resolvedSet(t, lock)
			got, err := ourSet(t, c.deps, idx)
			if err != nil {
				t.Fatalf("our resolver failed: %v", err)
			}
			if !sameSet(want, got) {
				t.Fatalf("resolution mismatch:\n bundler: %s\n ours:    %s", dumpSet(want), dumpSet(got))
			}
		})
	}
}

// TestOracleLockfileBytes checks that our serializer reproduces Bundler's
// Gemfile.lock byte-for-byte (after normalizing the host PLATFORMS line and the
// recorded BUNDLED WITH version, which depend on the runner).
func TestOracleLockfileBytes(t *testing.T) {
	bundle := bundleBin(t)
	if !pinnedBundle(t, bundle) {
		t.Skipf("bundler %s not installed; skipping byte-for-byte lock test", oracleBundlerVersion)
	}
	repo := t.TempDir()
	buildGemRepo(t, repo)
	idx := oracleIndex(t)

	lock, failed, errOut := bundleLock(t, bundle, repo, []string{`gem "d"`}, oracleBundlerVersion)
	if failed {
		t.Fatalf("bundler failed: %s", errOut)
	}
	bundlerLF, err := ParseLockfile(lock)
	if err != nil {
		t.Fatalf("parse bundler lock: %v", err)
	}

	// Rebuild the same lock from our resolution, reusing Bundler's source header
	// (the file:// remote), platforms and bundled-with so only the resolver +
	// serializer are under test.
	res, err := Resolve([]*Dependency{MustDependency("d")}, idx, bundlerLF.Sources[0].Source)
	if err != nil {
		t.Fatal(err)
	}
	def := &Definition{
		Dependencies: []*Dependency{MustDependency("d")},
		Platforms:    bundlerLF.Platforms,
		BundledWith:  bundlerLF.BundledWith,
		Source:       bundlerLF.Sources[0].Source,
	}
	ours := def.Lockfile(res).String()
	if ours != lock {
		t.Fatalf("lockfile bytes differ:\n--- bundler ---\n%s\n--- ours ---\n%s", lock, ours)
	}
}

// TestOracleConflict checks that where Bundler reports an unsatisfiable graph,
// our resolver also returns a VersionConflict (naming the same culprit gem).
func TestOracleConflict(t *testing.T) {
	bundle := bundleBin(t)
	if !pinnedBundle(t, bundle) {
		t.Skipf("bundler %s not installed; skipping conflict oracle", oracleBundlerVersion)
	}
	repo := t.TempDir()
	// Conflict corpus: x->z~>1.0, y->z~>2.0, z 1.0.0 + 2.0.0.
	saved := oracleCorpus
	oracleCorpus = []oracleGem{
		{name: "x", version: "1.0.0", deps: [][2]string{{"z", "~> 1.0"}}},
		{name: "y", version: "1.0.0", deps: [][2]string{{"z", "~> 2.0"}}},
		{name: "z", version: "1.0.0"},
		{name: "z", version: "2.0.0"},
	}
	defer func() { oracleCorpus = saved }()
	buildGemRepo(t, repo)
	idx := oracleIndex(t)

	_, failed, _ := bundleLock(t, bundle, repo, []string{`gem "x"`, `gem "y"`}, oracleBundlerVersion)
	if !failed {
		t.Fatal("expected bundler to fail resolution")
	}
	_, err := Resolve([]*Dependency{MustDependency("x"), MustDependency("y")}, idx, gemSource())
	if err == nil {
		t.Fatal("expected our resolver to conflict too")
	}
	if vc, ok := err.(*VersionConflict); !ok || vc.Name != "z" {
		t.Fatalf("want VersionConflict on z, got %v", err)
	}
}

// rubyBin resolves the `ruby` interpreter for the Gemfile-DSL oracle, skipping
// on Windows / where ruby is absent (qemu lanes).
func rubyBin(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Gemfile oracle skipped on Windows")
	}
	p, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("ruby not on PATH; skipping Gemfile DSL oracle")
	}
	return p
}

// TestOracleGemfileParity drives the real Bundler DSL (`Bundler::Dsl.evaluate`)
// over a canonical Gemfile and asserts ParseGemfile extracts the same gem
// name -> requirement / groups / source-kind model on the deterministic forms.
func TestOracleGemfileParity(t *testing.T) {
	ruby := rubyBin(t)
	gemfile := strings.Join([]string{
		`source "https://rubygems.org"`,
		`ruby "3.4.0"`,
		`gem "rake", "~> 13.0"`,
		`gem "rspec", ">= 3.0", "< 4.0", require: false`,
		`group :development, :test do`,
		`  gem "rubocop"`,
		`end`,
		`gem "nokogiri", git: "https://github.com/sparklemotion/nokogiri.git", branch: "main"`,
		`gem "local", path: "../local"`,
		`git_source(:github) { |repo| "https://github.com/#{repo}.git" }`,
		`gem "rails", github: "rails/rails"`,
	}, "\n") + "\n"

	work := t.TempDir()
	gfPath := filepath.Join(work, "Gemfile")
	if err := os.WriteFile(gfPath, []byte(gemfile), 0o644); err != nil {
		t.Fatal(err)
	}
	// Ask MRI Bundler to dump its model as name<TAB>req<TAB>groups<TAB>srckind.
	script := `require "bundler"
b = Bundler::Dsl.evaluate(ARGV[0], nil, {})
b.dependencies.sort_by(&:name).each do |d|
  kind = d.source ? d.source.class.name.split("::").last : "rubygems"
  groups = d.groups.map(&:to_s).sort.join(",")
  puts [d.name, d.requirement.to_s, groups, kind].join("\t")
end`
	cmd := exec.Command(ruby, "-e", script, gfPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("bundler DSL eval unavailable (%v):\n%s", err, out)
	}

	type row struct{ req, groups, kind string }
	want := map[string]row{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("unexpected oracle line %q", line)
		}
		want[f[0]] = row{f[1], f[2], f[3]}
	}

	gf, err := ParseGemfile(gemfile)
	if err != nil {
		t.Fatalf("ParseGemfile: %v", err)
	}
	if gf.RubyVersion != "3.4.0" {
		t.Fatalf("ruby version = %q", gf.RubyVersion)
	}
	got := map[string]row{}
	for _, d := range gf.Gems {
		groups := append([]string(nil), d.Groups...)
		sort.Strings(groups)
		kind := "rubygems"
		if d.Source != nil {
			switch d.Source.Type {
			case GitSource:
				kind = "Git"
			case PathSource:
				kind = "Path"
			}
		}
		got[d.Name] = row{d.Requirement.String(), strings.Join(groups, ","), kind}
	}

	if len(got) != len(want) {
		t.Fatalf("gem count: ours=%d bundler=%d", len(got), len(want))
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Fatalf("missing gem %q", name)
		}
		if g != w {
			t.Fatalf("gem %q mismatch:\n bundler: %+v\n ours:    %+v", name, w, g)
		}
	}
}

// ensure rubygems import is used even if the oracle is skipped (keeps the
// dependency edge explicit in this file).
var _ = rubygems.DefaultRequirement
