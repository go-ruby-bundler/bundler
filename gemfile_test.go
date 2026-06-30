// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import (
	"reflect"
	"strings"
	"testing"
)

// findGem returns the parsed dependency for name, or nil.
func findGem(gf *Gemfile, name string) *Dependency {
	for _, d := range gf.Gems {
		if d.Name == name {
			return d
		}
	}
	return nil
}

func mustParseGemfile(t *testing.T, src string) *Gemfile {
	t.Helper()
	gf, err := ParseGemfile(src)
	if err != nil {
		t.Fatalf("ParseGemfile: %v", err)
	}
	return gf
}

// TestGemfileSourceAndRuby parses source and ruby directives.
func TestGemfileSourceAndRuby(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
source 'https://gems.example.com'
ruby "3.4.0"
`)
	if !reflect.DeepEqual(gf.Sources, []string{"https://rubygems.org", "https://gems.example.com"}) {
		t.Fatalf("sources = %v", gf.Sources)
	}
	if gf.RubyVersion != "3.4.0" {
		t.Fatalf("ruby = %q", gf.RubyVersion)
	}
}

// TestGemfileGemForms covers bare, single- and multi-constraint gem lines.
func TestGemfileGemForms(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
gem "rake"
gem "rspec", "~> 3.0"
gem "puma", ">= 5.0", "< 7.0"
`)
	if d := findGem(gf, "rake"); d == nil || d.Requirement.String() != ">= 0" {
		t.Fatalf("rake req = %v", d)
	}
	if d := findGem(gf, "rspec"); d == nil || d.Requirement.String() != "~> 3.0" {
		t.Fatalf("rspec req = %v", d.Requirement)
	}
	if d := findGem(gf, "puma"); d == nil || d.Requirement.String() != ">= 5.0, < 7.0" {
		t.Fatalf("puma req = %q", d.Requirement.String())
	}
	// Default ungrouped gem belongs to :default (Bundler::Dependency default).
	if d := findGem(gf, "rake"); !reflect.DeepEqual(d.Groups, []string{"default"}) {
		t.Fatalf("rake groups = %v, want [default]", d.Groups)
	}
}

// TestGemfileGroups: a group block, the :group option, and the :groups option.
func TestGemfileGroups(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
group :development, :test do
  gem "rubocop"
  gem "rspec"
end
gem "byebug", group: :debug
gem "pry", groups: [:console, :debug]
`)
	if d := findGem(gf, "rubocop"); !reflect.DeepEqual(d.Groups, []string{"development", "test"}) {
		t.Fatalf("rubocop groups = %v", d.Groups)
	}
	if d := findGem(gf, "byebug"); !reflect.DeepEqual(d.Groups, []string{"debug"}) {
		t.Fatalf("byebug groups = %v", d.Groups)
	}
	if d := findGem(gf, "pry"); !reflect.DeepEqual(d.Groups, []string{"console", "debug"}) {
		t.Fatalf("pry groups = %v", d.Groups)
	}
}

// TestGemfilePlatforms covers :platform / :platforms options and a platforms
// block.
func TestGemfilePlatforms(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
gem "ffi", platform: :mri
gem "jdbc", platforms: [:jruby]
platforms :ruby do
  gem "unicorn"
end
`)
	if d := findGem(gf, "ffi"); !reflect.DeepEqual(d.Platforms, []string{"mri"}) {
		t.Fatalf("ffi platforms = %v", d.Platforms)
	}
	if d := findGem(gf, "jdbc"); !reflect.DeepEqual(d.Platforms, []string{"jruby"}) {
		t.Fatalf("jdbc platforms = %v", d.Platforms)
	}
	if findGem(gf, "unicorn") == nil {
		t.Fatal("unicorn missing from platforms block")
	}
}

// TestGemfileGitPathSources covers :git (+ branch/ref/tag/submodules) and :path.
func TestGemfileGitPathSources(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
gem "nokogiri", git: "https://github.com/sparklemotion/nokogiri.git", branch: "main"
gem "local", path: "../local"
gem "pinned", git: "https://example.com/p.git", ref: "abc123", tag: "v1", submodules: true
`)
	n := findGem(gf, "nokogiri")
	if n.Source == nil || n.Source.Type != GitSource || n.Source.GemRemote() != "https://github.com/sparklemotion/nokogiri.git" {
		t.Fatalf("nokogiri source = %+v", n.Source)
	}
	if n.Source.Branch != "main" {
		t.Fatalf("nokogiri branch = %q", n.Source.Branch)
	}
	if !n.pinned() {
		t.Fatal("git gem should be pinned")
	}
	l := findGem(gf, "local")
	if l.Source == nil || l.Source.Type != PathSource || l.Source.GemRemote() != "../local" {
		t.Fatalf("local source = %+v", l.Source)
	}
	p := findGem(gf, "pinned")
	if p.Source.Ref != "abc123" || p.Source.Tag != "v1" || p.Source.Submodules != "true" {
		t.Fatalf("pinned git opts = %+v", p.Source)
	}
}

// TestGemfileGitSourceCustom covers git_source registration and its use.
func TestGemfileGitSourceCustom(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
git_source(:github) { |repo| "https://github.com/#{repo}.git" }
gem "rails", github: "rails/rails"
gem "sidekiq", github: "mperham/sidekiq", branch: "main"
`)
	if got := gf.GitSources["github"]; got != "https://github.com/%s.git" {
		t.Fatalf("github template = %q", got)
	}
	r := findGem(gf, "rails")
	if r.Source == nil || r.Source.Type != GitSource || r.Source.GemRemote() != "https://github.com/rails/rails.git" {
		t.Fatalf("rails source = %+v", r.Source)
	}
	s := findGem(gf, "sidekiq")
	if s.Source.GemRemote() != "https://github.com/mperham/sidekiq.git" || s.Source.Branch != "main" {
		t.Fatalf("sidekiq source = %+v", s.Source)
	}
}

// TestGemfileGemspec covers the gemspec directive with and without options.
func TestGemfileGemspec(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
gemspec
gemspec path: "engines/admin", name: "admin", development_group: "dev"
`)
	if len(gf.Gemspecs) != 2 {
		t.Fatalf("gemspecs = %d", len(gf.Gemspecs))
	}
	if gf.Gemspecs[0].Path != "." || gf.Gemspecs[0].DevelopmentGroup != "development" {
		t.Fatalf("gemspec[0] = %+v", gf.Gemspecs[0])
	}
	if gf.Gemspecs[1].Path != "engines/admin" || gf.Gemspecs[1].Name != "admin" || gf.Gemspecs[1].DevelopmentGroup != "dev" {
		t.Fatalf("gemspec[1] = %+v", gf.Gemspecs[1])
	}
}

// TestGemfileRequireAndHashRocket covers the require: option (ignored for
// resolution) and the legacy ":sym => val" hash form.
func TestGemfileRequireAndHashRocket(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
gem "rspec", require: false
gem "legacy", :group => :test
gem "quoted", :"odd-key" => "x", git: "https://e.com/q.git"
`)
	if findGem(gf, "rspec") == nil {
		t.Fatal("rspec missing")
	}
	if d := findGem(gf, "legacy"); !reflect.DeepEqual(d.Groups, []string{"test"}) {
		t.Fatalf("legacy groups = %v", d.Groups)
	}
}

// TestGemfileCommentsAndBlankLines verifies comments (incl. # in strings) and
// blank lines are handled.
func TestGemfileCommentsAndBlankLines(t *testing.T) {
	gf := mustParseGemfile(t, `
# a leading comment
source "https://rubygems.org"  # trailing comment

gem "rake" # use rake
gem "weird", git: "https://e.com/has#fragment.git"
`)
	if findGem(gf, "rake") == nil {
		t.Fatal("rake missing")
	}
	if d := findGem(gf, "weird"); d.Source.GemRemote() != "https://e.com/has#fragment.git" {
		t.Fatalf("weird source url = %q (# inside string mis-stripped?)", d.Source.GemRemote())
	}
}

// TestGemfileDependencies returns the deps slice for the resolver.
func TestGemfileDependencies(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
gem "rake"
gem "rspec", "~> 3.0"
`)
	deps := gf.Dependencies()
	if len(deps) != 2 {
		t.Fatalf("deps = %d", len(deps))
	}
}

// TestGemfileSingleQuoteEscapes covers \\ and \' in single-quoted strings and
// \n/\t/\" in double-quoted strings.
func TestGemfileSingleQuoteEscapes(t *testing.T) {
	gf := mustParseGemfile(t, `
source 'https://a\\b'
gem "x", path: 'a\'b'
gem "y", path: "tab\there"
`)
	if gf.Sources[0] != `https://a\b` {
		t.Fatalf("source escape = %q", gf.Sources[0])
	}
	if d := findGem(gf, "x"); d.Source.GemRemote() != "a'b" {
		t.Fatalf("x path = %q", d.Source.GemRemote())
	}
	if d := findGem(gf, "y"); d.Source.GemRemote() != "tab\there" {
		t.Fatalf("y path = %q", d.Source.GemRemote())
	}
}

// TestGemfileErrors covers the error paths.
func TestGemfileErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"unknown directive", "frobnicate :x", "unrecognized"},
		{"unterminated group", "group :test do\n  gem \"x\"", "unterminated"},
		{"stray end", "end", "unexpected end"},
		{"gem no name", "gem", "missing name"},
		{"gem dynamic name", "gem foo", "quoted string"},
		{"interpolated name", `gem "#{x}"`, "dynamic"},
		{"source no arg", "source", "string literal"},
		{"ruby dynamic file", `ruby file: ".ruby-version"`, "dynamic"},
		{"ruby RUBY_VERSION", "ruby RUBY_VERSION", "dynamic"},
		{"scoped source block", "source \"https://x\" do\nend", "scoped"},
		{"group no name", "group do\nend", "missing group"},
		{"group no block", "group :test", "do ... end"},
		{"platforms no block", "platforms :ruby", "do ... end"},
		{"install_if", "install_if -> { true } do\nend", "dynamic"},
		{"git_source no paren", "git_source :x", "(:name)"},
		{"git_source no block", "git_source(:x)", "{ ... }"},
		{"git_source no name", "git_source() { |r| \"x\" }", "missing name"},
		{"git_source dynamic tmpl", "git_source(:x) { |r| foo }", "double-quoted"},
		{"unbalanced", `gem "x", [1, 2`, "unbalanced"},
		{"unterminated string", `gem "x`, "unterminated string"},
		{"gemspec bad opt", `gemspec "x"`, "expected option"},
		{"gemspec unknown opt", `gemspec foo: "x"`, "unknown option"},
		{"gemspec dynamic opt", `gemspec path: foo`, "quoted string"},
		{"bad requirement", `gem "x", "not a req"`, "requirement"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseGemfile(c.src)
			if err == nil {
				t.Fatalf("want error containing %q, got nil", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), c.want)
			}
			// GemfileError carries a line number.
			if ge, ok := err.(*GemfileError); ok && ge.Line == 0 {
				t.Fatalf("GemfileError has zero line: %v", ge)
			}
		})
	}
}

// TestGemfileToResolver wires a parsed Gemfile through the resolver.
func TestGemfileToResolver(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
gem "d"
`)
	idx := buildIndex(t)
	res, err := Resolve(gf.Dependencies(), idx, gemSource())
	if err != nil {
		t.Fatal(err)
	}
	if res.Get("a").Version.String() != "1.0.0" {
		t.Fatalf("a = %s", res.Get("a").Version.String())
	}
}

// TestGemfileErrorString checks the GemfileError message format.
func TestGemfileErrorString(t *testing.T) {
	e := &GemfileError{Line: 7, msg: "boom"}
	if got := e.Error(); got != "Gemfile:7: boom" {
		t.Fatalf("error string = %q", got)
	}
}

// TestGemfileBareWordValues exercises the bare-word fallback in source-string
// option values (path:/git:/custom-source slug given as an unquoted token keeps
// the raw token) and the symbol-list string element.
func TestGemfileBareWordValues(t *testing.T) {
	// git_source slug given as a bare word -> raw token interpolated.
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
git_source(:gh) { |repo| "https://github.com/#{repo}.git" }
gem "x", gh: bareword
gem "y", path: barepath
gem "z", git: baregit, branch: barebranch
`)
	if got := findGem(gf, "x").Source.GemRemote(); got != "https://github.com/bareword.git" {
		t.Fatalf("x source = %q", got)
	}
	if got := findGem(gf, "y").Source.GemRemote(); got != "barepath" {
		t.Fatalf("y source = %q", got)
	}
	z := findGem(gf, "z").Source
	if z.GemRemote() != "baregit" || z.Branch != "barebranch" {
		t.Fatalf("z source = %+v", z)
	}
}

// TestGemfileGroupStringAndArrayMix covers group names given as strings and
// mixed arrays.
func TestGemfileGroupStringAndArrayMix(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
gem "a", group: "test"
gem "b", groups: [:x, "y"]
gem "c", platforms: %i[mri jruby]
`)
	if d := findGem(gf, "a"); !reflect.DeepEqual(d.Groups, []string{"test"}) {
		t.Fatalf("a groups = %v", d.Groups)
	}
	if d := findGem(gf, "b"); !reflect.DeepEqual(d.Groups, []string{"x", "y"}) {
		t.Fatalf("b groups = %v", d.Groups)
	}
	if d := findGem(gf, "c"); !reflect.DeepEqual(d.Platforms, []string{"mri", "jruby"}) {
		t.Fatalf("c platforms = %v", d.Platforms)
	}
}

// TestGemfileDedupeGroups verifies duplicate group names collapse.
func TestGemfileDedupeGroups(t *testing.T) {
	gf := mustParseGemfile(t, `
source "https://rubygems.org"
group :test do
  gem "a", group: :test
end
`)
	if d := findGem(gf, "a"); !reflect.DeepEqual(d.Groups, []string{"test"}) {
		t.Fatalf("a groups = %v, want deduped [test]", d.Groups)
	}
}

// TestGemfileDoubleQuoteEscapes covers \n and \t and the default-escape branch
// in double-quoted strings.
func TestGemfileDoubleQuoteEscapes(t *testing.T) {
	gf := mustParseGemfile(t, "source \"a\\nb\"\ngem \"x\", path: \"p\\qr\"\n")
	if gf.Sources[0] != "a\nb" {
		t.Fatalf("source = %q", gf.Sources[0])
	}
	// \q is not a known escape; Ruby keeps the q.
	if got := findGem(gf, "x").Source.GemRemote(); got != "pqr" {
		t.Fatalf("x path = %q", got)
	}
}

// TestGemfileInterpolatedTemplateUnterminated covers the unterminated-#{ path.
func TestGemfileInterpolatedTemplateUnterminated(t *testing.T) {
	_, err := ParseGemfile(`git_source(:x) { |r| "https://h/#{r" }`)
	if err == nil || !strings.Contains(err.Error(), "unterminated interpolation") {
		t.Fatalf("want unterminated interpolation, got %v", err)
	}
}

// TestGemfileEmptySymbolList covers parseSymbolList returning nil for junk.
func TestGemfileEmptySymbolList(t *testing.T) {
	// A group whose only token is a non-symbol/non-string yields no groups.
	_, err := ParseGemfile("group 123 do\nend")
	if err == nil || !strings.Contains(err.Error(), "missing group") {
		t.Fatalf("want missing group, got %v", err)
	}
}

// TestGemfileMoreErrorPaths exercises the remaining error branches.
func TestGemfileMoreErrorPaths(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		// ruby with a bare-word arg (no quotes, no file:/RUBY_VERSION).
		{"ruby bare word", "ruby foo", "quoted string"},
		// gem requirement given as a bare word (not an option, not a string).
		{"gem bareword req", `gem "x", foo`, "requirement"},
		// group head with an unbalanced bracket -> splitArgs error.
		{"group unbalanced", "group [a do\nend", "unbalanced"},
		// gemspec with an unbalanced bracket -> splitArgs error.
		{"gemspec unbalanced", "gemspec [a", "unbalanced"},
		// source with an unbalanced bracket -> nextString splitArgs error.
		{"source unbalanced", "source [a", "unbalanced"},
		// nested symbol-list with an unbalanced bracket is dropped (group sees no
		// names) -> missing group name.
		{"group nested unbalanced", "group [:a, [b do\nend", "unbalanced"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseGemfile(c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("src %q: want %q, got %v", c.src, c.want, err)
			}
		})
	}
}

// TestGemfileHelperEdges covers small lexical-helper branches directly.
func TestGemfileHelperEdges(t *testing.T) {
	// parseSymbol of a bare ":" yields "" (empty name -> isIdent("") false).
	if got := parseSymbol(":"); got != "" {
		t.Fatalf("parseSymbol(:) = %q, want empty", got)
	}
	// isIdent on a non-word char.
	if isIdent("a b") {
		t.Fatal("isIdent should reject space")
	}
	// parseSymbolList of a bracketed token whose inner content fails to split
	// (unterminated string) returns nil.
	if got := parseSymbolList(`[:a, "b]`); got != nil {
		t.Fatalf("parseSymbolList(unbalanced inner) = %v, want nil", got)
	}
	// single-quoted string with a backslash before a non-special char keeps the
	// backslash (Ruby single-quote semantics).
	gf := mustParseGemfile(t, `gem "x", path: 'a\b'`+"\n")
	if got := findGem(gf, "x").Source.GemRemote(); got != `a\b` {
		t.Fatalf("x path = %q, want a\\b", got)
	}
}

// TestGemfileHashRocketEdgeCases covers parseOption's =>/colon edge branches.
func TestGemfileHashRocketEdgeCases(t *testing.T) {
	// "=> value" with a non-symbol key: the token is treated as a (string)
	// requirement, which fails to parse as a requirement.
	if _, err := ParseGemfile(`gem "x", "a" => :b`); err == nil ||
		!strings.Contains(err.Error(), "requirement") {
		t.Fatalf("want requirement error, got %v", err)
	}
	// A colon-prefixed token with no value is a plain symbol requirement attempt.
	if _, err := ParseGemfile(`gem "x", :sym`); err == nil ||
		!strings.Contains(err.Error(), "quoted string") {
		t.Fatalf("want quoted-string error for bare symbol arg, got %v", err)
	}
}
