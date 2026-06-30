// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import (
	"fmt"
	"strings"

	"github.com/go-ruby-rubygems/rubygems"
)

// Gemfile is the parsed model of a Bundler Gemfile (the DSL the host evaluates
// to drive resolution). It is the structured result of reading the canonical,
// deterministic DSL forms — the ones a `bundle lock` cares about — without a
// Ruby interpreter.
//
// A Gemfile is, in full generality, arbitrary Ruby; ParseGemfile reads the
// common static forms (string literals, symbols, arrays, hash options) and
// reports anything it cannot statically understand. For Gemfiles that compute
// gem names or requirements at runtime, the host evaluates the Ruby (e.g. rbgo)
// and builds the []*Dependency directly.
type Gemfile struct {
	// Sources is the list of `source "url"` URLs in declaration order. The first
	// is Bundler's primary remote.
	Sources []string
	// RubyVersion is the `ruby "x.y.z"` constraint ("" when absent).
	RubyVersion string
	// Gems is the declared dependencies in declaration order, each carrying its
	// requirement, groups, platforms and (git/path) source options.
	Gems []*Dependency
	// Gemspecs holds each `gemspec` directive (development gemspec inclusion).
	Gemspecs []GemspecDirective
	// GitSources maps a custom `git_source(:name){ |repo| url }` name to its URL
	// template (with "%s" where the repo slug is interpolated).
	GitSources map[string]string
}

// GemspecDirective is a `gemspec` line: it pulls a local .gemspec's runtime
// dependencies into the Gemfile. The options (path/name/development_group) are
// captured; resolving the actual gemspec is a host-side seam.
type GemspecDirective struct {
	Path             string // :path option (default ".")
	Name             string // :name option (default: the single gemspec found)
	DevelopmentGroup string // :development_group option (default "development")
}

// GemfileError reports a Gemfile DSL line that could not be statically parsed.
type GemfileError struct {
	Line int
	msg  string
}

func (e *GemfileError) Error() string {
	return fmt.Sprintf("Gemfile:%d: %s", e.Line, e.msg)
}

// ParseGemfile reads the canonical, deterministic forms of a Bundler Gemfile:
//
//   - source "https://rubygems.org"
//   - ruby "3.4.0"  (and ruby file: / RUBY_VERSION forms are reported as dynamic)
//   - gem "name"[, "req"...][, opt: val...]
//   - group :a, :b do ... end   (nested gem lines inherit the groups)
//   - gemspec [path: "...", name: "...", development_group: "..."]
//   - git_source(:name) { |repo| "https://host/#{repo}.git" }
//
// gem option keys understood: :group/:groups, :require, :platform/:platforms,
// :git, :path, :branch, :ref, :tag, :submodules, :source. Comments (#...) and
// blank lines are skipped. A form it cannot statically read yields a
// *GemfileError naming the offending line, rather than silently dropping it.
func ParseGemfile(content string) (*Gemfile, error) {
	gf := &Gemfile{GitSources: map[string]string{}}
	p := &gemfileParser{gf: gf}
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		if err := p.line(lines[i], i+1); err != nil {
			return nil, err
		}
	}
	if len(p.groupStack) != 0 {
		return nil, &GemfileError{len(lines), "unterminated group/source block (missing end)"}
	}
	return gf, nil
}

type gemfileParser struct {
	gf         *Gemfile
	groupStack [][]string // active `group` blocks; current groups = top of stack
}

// curGroups is the groups in effect from enclosing `group` blocks.
func (p *gemfileParser) curGroups() []string {
	if len(p.groupStack) == 0 {
		return nil
	}
	return p.groupStack[len(p.groupStack)-1]
}

func (p *gemfileParser) line(raw string, n int) error {
	line := stripComment(raw)
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	// `end` closes the innermost group/source block.
	if line == "end" {
		if len(p.groupStack) == 0 {
			return &GemfileError{n, "unexpected end (no open block)"}
		}
		p.groupStack = p.groupStack[:len(p.groupStack)-1]
		return nil
	}

	head, rest := splitHead(line)
	switch head {
	case "source":
		return p.source(rest, n)
	case "ruby":
		return p.ruby(rest, n)
	case "gem":
		return p.gem(rest, n)
	case "group":
		return p.group(rest, n)
	case "gemspec":
		return p.gemspec(rest, n)
	case "git_source":
		return p.gitSource(rest, n)
	case "platforms", "platform":
		// `platforms :x do ... end` — treat like a group block for nesting, but
		// it adds platforms, not groups. We push an empty groups frame so the
		// matching `end` balances; platform scoping of nested gems is dynamic, so
		// we report it if it actually carries gems? No — simplest faithful
		// behavior: support the block form by pushing a frame.
		return p.platformsBlock(rest, n)
	case "install_if", "env":
		// Conditional blocks whose predicate is dynamic.
		return &GemfileError{n, fmt.Sprintf("dynamic Gemfile directive %q not statically parseable", head)}
	default:
		return &GemfileError{n, fmt.Sprintf("unrecognized Gemfile directive %q", head)}
	}
}

func (p *gemfileParser) source(rest string, n int) error {
	url, err := nextString(rest)
	if err != nil {
		return &GemfileError{n, "source: " + err.Error()}
	}
	// `source "url" do ... end` (scoped source block) is dynamic scoping; reject
	// the block form explicitly rather than mis-grouping its gems.
	if endsWithDo(rest) {
		return &GemfileError{n, "scoped `source ... do` block not statically parseable"}
	}
	p.gf.Sources = append(p.gf.Sources, url)
	return nil
}

func (p *gemfileParser) ruby(rest string, n int) error {
	if strings.Contains(rest, "file:") || strings.Contains(rest, "RUBY_VERSION") {
		return &GemfileError{n, "dynamic `ruby` directive (file:/RUBY_VERSION) not statically parseable"}
	}
	v, err := nextString(rest)
	if err != nil {
		return &GemfileError{n, "ruby: " + err.Error()}
	}
	p.gf.RubyVersion = v
	return nil
}

func (p *gemfileParser) gem(rest string, n int) error {
	args, err := splitArgs(rest)
	if err != nil {
		return &GemfileError{n, "gem: " + err.Error()}
	}
	if len(args) == 0 {
		return &GemfileError{n, "gem: missing name"}
	}
	name, err := parseStringLit(args[0])
	if err != nil {
		return &GemfileError{n, "gem name: " + err.Error()}
	}

	var constraints []string
	opts := map[string]string{}
	for _, a := range args[1:] {
		if k, v, ok := parseOption(a); ok {
			opts[k] = v
			continue
		}
		c, err := parseStringLit(a)
		if err != nil {
			return &GemfileError{n, "gem requirement: " + err.Error()}
		}
		constraints = append(constraints, c)
	}

	dep := &Dependency{Name: name}
	if len(constraints) == 0 {
		dep.Requirement = rubygems.DefaultRequirement()
	} else {
		req, err := rubygems.NewRequirement(constraints...)
		if err != nil {
			return &GemfileError{n, "gem requirement: " + err.Error()}
		}
		dep.Requirement = req
	}

	// Groups: enclosing group blocks plus any :group/:groups option.
	groups := append([]string(nil), p.curGroups()...)
	if g, ok := opts["group"]; ok {
		groups = append(groups, parseSymbolList(g)...)
	}
	if g, ok := opts["groups"]; ok {
		groups = append(groups, parseSymbolList(g)...)
	}
	if len(groups) == 0 {
		// Bundler::Dependency defaults an ungrouped gem to the :default group.
		groups = []string{"default"}
	}
	dep.Groups = dedupeStrings(groups)

	if pl, ok := opts["platform"]; ok {
		dep.Platforms = append(dep.Platforms, parseSymbolList(pl)...)
	}
	if pl, ok := opts["platforms"]; ok {
		dep.Platforms = append(dep.Platforms, parseSymbolList(pl)...)
	}

	// git/path source options pin the dependency.
	if src := p.depSource(opts); src != nil {
		dep.Source = src
	}

	p.gf.Gems = append(p.gf.Gems, dep)
	return nil
}

// depSource builds the pinned Source from a gem's git/path options, or nil for a
// plain GEM dependency. :branch/:ref/:tag/:submodules attach to a :git source.
func (p *gemfileParser) depSource(opts map[string]string) *Source {
	if path, ok := opts["path"]; ok {
		url, err := parseStringLit(path)
		if err != nil {
			url = path
		}
		return &Source{Type: PathSource, Remotes: []string{url}}
	}
	if git, ok := opts["git"]; ok {
		url, err := parseStringLit(git)
		if err != nil {
			url = git
		}
		s := &Source{Type: GitSource, Remotes: []string{url}}
		s.Branch = optString(opts, "branch")
		s.Ref = optString(opts, "ref")
		s.Tag = optString(opts, "tag")
		s.Submodules = optString(opts, "submodules")
		return s
	}
	// A custom git_source name used as gem "x", custom_name: "owner/repo".
	for name, tmpl := range p.gf.GitSources {
		if v, ok := opts[name]; ok {
			slug, err := parseStringLit(v)
			if err != nil {
				slug = v
			}
			s := &Source{Type: GitSource, Remotes: []string{strings.ReplaceAll(tmpl, "%s", slug)}}
			s.Branch = optString(opts, "branch")
			s.Ref = optString(opts, "ref")
			s.Tag = optString(opts, "tag")
			return s
		}
	}
	return nil
}

func optString(opts map[string]string, k string) string {
	v, ok := opts[k]
	if !ok {
		return ""
	}
	s, err := parseStringLit(v)
	if err != nil {
		return v
	}
	return s
}

func (p *gemfileParser) group(rest string, n int) error {
	if !endsWithDo(rest) {
		return &GemfileError{n, "group requires a `do ... end` block"}
	}
	head := strings.TrimSuffix(strings.TrimSpace(rest), "do")
	args, err := splitArgs(strings.TrimSpace(head))
	if err != nil {
		return &GemfileError{n, "group: " + err.Error()}
	}
	var groups []string
	for _, a := range args {
		groups = append(groups, parseSymbolList(a)...)
	}
	if len(groups) == 0 {
		return &GemfileError{n, "group: missing group name(s)"}
	}
	p.groupStack = append(p.groupStack, groups)
	return nil
}

func (p *gemfileParser) platformsBlock(rest string, n int) error {
	if !endsWithDo(rest) {
		return &GemfileError{n, "platforms requires a `do ... end` block"}
	}
	// Push an empty groups frame so the matching `end` balances. Nested gems keep
	// their enclosing groups; platform scoping of those gems is not applied
	// statically (it is a host concern), so this is a structural no-op.
	p.groupStack = append(p.groupStack, p.curGroups())
	return nil
}

func (p *gemfileParser) gemspec(rest string, n int) error {
	d := GemspecDirective{Path: ".", DevelopmentGroup: "development"}
	if strings.TrimSpace(rest) != "" {
		args, err := splitArgs(rest)
		if err != nil {
			return &GemfileError{n, "gemspec: " + err.Error()}
		}
		for _, a := range args {
			k, v, ok := parseOption(a)
			if !ok {
				return &GemfileError{n, "gemspec: expected option, got " + a}
			}
			s, err := parseStringLit(v)
			if err != nil {
				return &GemfileError{n, "gemspec option: " + err.Error()}
			}
			switch k {
			case "path":
				d.Path = s
			case "name":
				d.Name = s
			case "development_group":
				d.DevelopmentGroup = s
			default:
				return &GemfileError{n, "gemspec: unknown option " + k}
			}
		}
	}
	p.gf.Gemspecs = append(p.gf.Gemspecs, d)
	return nil
}

// gitSource parses `git_source(:name) { |repo| "https://.../#{repo}.git" }`,
// recording a "%s" template for the repo slug.
func (p *gemfileParser) gitSource(rest string, n int) error {
	open := strings.IndexByte(rest, '(')
	close := strings.IndexByte(rest, ')')
	if open != 0 || close < 0 {
		return &GemfileError{n, "git_source: expected (:name) { |repo| url }"}
	}
	name := parseSymbol(strings.TrimSpace(rest[1:close]))
	if name == "" {
		return &GemfileError{n, "git_source: missing name symbol"}
	}
	body := rest[close+1:]
	lb := strings.IndexByte(body, '{')
	rb := strings.LastIndexByte(body, '}')
	if lb < 0 || rb < 0 || rb < lb {
		return &GemfileError{n, "git_source: missing { ... } block"}
	}
	block := body[lb+1 : rb]
	// strip the |repo| param.
	if bar := strings.IndexByte(block, '|'); bar >= 0 {
		if bar2 := strings.IndexByte(block[bar+1:], '|'); bar2 >= 0 {
			block = block[bar+1+bar2+1:]
		}
	}
	tmpl, err := parseInterpolatedString(strings.TrimSpace(block))
	if err != nil {
		return &GemfileError{n, "git_source: " + err.Error()}
	}
	p.gf.GitSources[name] = tmpl
	return nil
}

// Dependencies converts the parsed Gemfile gems into the []*Dependency the
// resolver and lockfile production consume.
func (gf *Gemfile) Dependencies() []*Dependency {
	return append([]*Dependency(nil), gf.Gems...)
}

// --- lexical helpers ---

// stripComment removes a trailing # comment, respecting string literals so a #
// inside quotes is not treated as a comment.
func stripComment(s string) string {
	inS, inD := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && (inS || inD):
			i++ // skip escaped char
		case c == '\'' && !inD:
			inS = !inS
		case c == '"' && !inS:
			inD = !inD
		case c == '#' && !inS && !inD:
			return s[:i]
		}
	}
	return s
}

// splitHead splits a directive into its leading word and the remainder. It
// handles both "gem \"x\"" and "git_source(:x)" (no space before paren).
func splitHead(line string) (head, rest string) {
	i := 0
	for i < len(line) && (isWordByte(line[i])) {
		i++
	}
	head = line[:i]
	// Keep any leading "(" (git_source(:name)) attached to rest.
	rest = strings.TrimSpace(line[i:])
	return head, rest
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// endsWithDo reports whether a line ends with a `do` block opener.
func endsWithDo(s string) bool {
	t := strings.TrimSpace(s)
	return t == "do" || strings.HasSuffix(t, " do") || strings.HasSuffix(t, "\tdo")
}

// nextString reads the first string literal in s.
func nextString(s string) (string, error) {
	args, err := splitArgs(s)
	if err != nil {
		return "", err
	}
	if len(args) == 0 {
		return "", fmt.Errorf("expected a string literal")
	}
	return parseStringLit(args[0])
}

// splitArgs splits a comma-separated argument list at top level, respecting
// quotes, brackets and braces. It also strips a trailing `do` block opener.
func splitArgs(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	// drop a trailing "do" opener (group/source blocks).
	if endsWithDo(s) {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "do"))
	}
	if s == "" {
		return nil, nil
	}
	var args []string
	var cur strings.Builder
	depth := 0
	inS, inD := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && (inS || inD):
			cur.WriteByte(c)
			if i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			}
		case c == '\'' && !inD:
			inS = !inS
			cur.WriteByte(c)
		case c == '"' && !inS:
			inD = !inD
			cur.WriteByte(c)
		case (c == '[' || c == '{' || c == '(') && !inS && !inD:
			depth++
			cur.WriteByte(c)
		case (c == ']' || c == '}' || c == ')') && !inS && !inD:
			depth--
			cur.WriteByte(c)
		case c == ',' && depth == 0 && !inS && !inD:
			args = append(args, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if inS || inD {
		return nil, fmt.Errorf("unterminated string literal")
	}
	if depth != 0 {
		return nil, fmt.Errorf("unbalanced brackets")
	}
	if strings.TrimSpace(cur.String()) != "" {
		args = append(args, strings.TrimSpace(cur.String()))
	}
	return args, nil
}

// parseOption detects an option argument, in either "key: value" or
// ":key => value" form, returning the symbol key and the raw value token.
func parseOption(a string) (key, val string, ok bool) {
	a = strings.TrimSpace(a)
	if idx := strings.Index(a, "=>"); idx >= 0 {
		k := strings.TrimSpace(a[:idx])
		v := strings.TrimSpace(a[idx+2:])
		k = parseSymbol(k)
		if k == "" {
			return "", "", false
		}
		return k, v, true
	}
	// "key: value" — the key is a bare identifier followed by ':'.
	for i := 0; i < len(a); i++ {
		c := a[i]
		if c == ':' {
			k := strings.TrimSpace(a[:i])
			if k == "" || !isIdent(k) {
				return "", "", false
			}
			return k, strings.TrimSpace(a[i+1:]), true
		}
		if !isWordByte(c) {
			return "", "", false
		}
	}
	return "", "", false
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isWordByte(s[i]) {
			return false
		}
	}
	return true
}

// parseStringLit parses a single- or double-quoted Ruby string literal (no
// interpolation, the deterministic case). A bare %w array element is handled by
// the caller.
func parseStringLit(tok string) (string, error) {
	tok = strings.TrimSpace(tok)
	if len(tok) >= 2 && (tok[0] == '"' || tok[0] == '\'') && tok[len(tok)-1] == tok[0] {
		inner := tok[1 : len(tok)-1]
		if strings.Contains(inner, "#{") {
			return "", fmt.Errorf("interpolated string %q is dynamic", tok)
		}
		return unescape(inner, tok[0]), nil
	}
	return "", fmt.Errorf("expected a quoted string, got %q", tok)
}

// parseInterpolatedString turns "https://h/#{repo}.git" into a "%s" template.
func parseInterpolatedString(tok string) (string, error) {
	tok = strings.TrimSpace(tok)
	if len(tok) < 2 || tok[0] != '"' || tok[len(tok)-1] != '"' {
		return "", fmt.Errorf("expected a double-quoted template, got %q", tok)
	}
	inner := tok[1 : len(tok)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '#' && i+1 < len(inner) && inner[i+1] == '{' {
			end := strings.IndexByte(inner[i:], '}')
			if end < 0 {
				return "", fmt.Errorf("unterminated interpolation")
			}
			b.WriteString("%s")
			i += end
			continue
		}
		b.WriteByte(inner[i])
	}
	return b.String(), nil
}

// unescape applies the minimal Ruby escapes that occur in version/source
// strings. For single-quoted strings only \\ and \' are special.
func unescape(s string, quote byte) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			next := s[i+1]
			if quote == '\'' {
				if next == '\\' || next == '\'' {
					b.WriteByte(next)
					i++
					continue
				}
				b.WriteByte('\\')
				continue
			}
			switch next {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				// Any other escaped char (incl. \" and \\) is the char itself.
				b.WriteByte(next)
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseSymbol reads a Ruby symbol (":name" or "name:") to its bare name, or ""
// if the token is not a symbol.
func parseSymbol(tok string) string {
	tok = strings.TrimSpace(tok)
	if strings.HasPrefix(tok, ":") {
		name := tok[1:]
		// quoted symbol :"name"
		if len(name) >= 2 && (name[0] == '"' || name[0] == '\'') && name[len(name)-1] == name[0] {
			return name[1 : len(name)-1]
		}
		if isIdent(name) {
			return name
		}
	}
	return ""
}

// parseSymbolList reads either a single symbol or a [:a, :b] / %i[a b] array of
// symbols into a slice of bare names.
func parseSymbolList(tok string) []string {
	tok = strings.TrimSpace(tok)
	if name := parseSymbol(tok); name != "" {
		return []string{name}
	}
	if s := parseStringLit2(tok); s != "" {
		return []string{s}
	}
	// [:a, :b, "c"]
	if strings.HasPrefix(tok, "[") && strings.HasSuffix(tok, "]") {
		inner := tok[1 : len(tok)-1]
		parts, err := splitArgs(inner)
		if err != nil {
			return nil
		}
		var out []string
		for _, p := range parts {
			if name := parseSymbol(p); name != "" {
				out = append(out, name)
			} else if s := parseStringLit2(p); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	// %i[a b c] / %w[a b c]
	if strings.HasPrefix(tok, "%i[") || strings.HasPrefix(tok, "%w[") {
		inner := strings.TrimSuffix(tok[3:], "]")
		return strings.Fields(inner)
	}
	return nil
}

// parseStringLit2 is parseStringLit returning "" instead of an error, for the
// best-effort symbol-list path.
func parseStringLit2(tok string) string {
	s, err := parseStringLit(tok)
	if err != nil {
		return ""
	}
	return s
}

// dedupeStrings removes duplicate group/platform names, preserving first-seen
// order. Callers pass at least one element.
func dedupeStrings(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
