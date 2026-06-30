// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/go-ruby-rubygems/rubygems"
)

// ParseError is returned when a Gemfile.lock cannot be parsed.
type ParseError struct{ msg string }

func (e *ParseError) Error() string { return e.msg }

// section headers, mirroring Bundler::LockfileParser's constants.
const (
	secGit       = "GIT"
	secGem       = "GEM"
	secPath      = "PATH"
	secPlatforms = "PLATFORMS"
	secDeps      = "DEPENDENCIES"
	secRuby      = "RUBY VERSION"
	secBundled   = "BUNDLED WITH"
)

// optionRE matches a "  key: value" option line under a source header (Bundler
// OPTIONS = /^  ([a-z]+): (.*)$/i).
var optionRE = regexp.MustCompile(`^  ([a-zA-Z]+): (.*)$`)

// nameVersionRE matches a spec / dependency / nested-dependency line. It mirrors
// Bundler's NAME_VERSION: the leading indentation (group 1, validated to be 2,
// 4 or 6 spaces by the caller), the name (group 2), an optional
// " (version[-platform])" (groups 3 and 4), and an optional "!" pinned marker
// (group 5). RE2 has no negative lookahead, so the indent is captured greedily
// and its size is checked in Go.
var nameVersionRE = regexp.MustCompile(`^( +)(.*?)(?: \(([^-]*)(?:-(.*))?\))?(!)?$`)

// ParseLockfile parses Gemfile.lock content into a Lockfile. It is byte-for-byte
// invertible: l.String() of the result reproduces the input for a well-formed
// Bundler lock.
func ParseLockfile(content string) (*Lockfile, error) {
	if mergeConflictRE.MatchString(content) {
		return nil, &ParseError{"lockfile contains merge conflicts"}
	}
	p := &parser{lf: &Lockfile{}, specsByFull: map[string]*Spec{}}

	// Split into lines, dropping the trailing empty element from a final newline,
	// the way Bundler iterates line-by-line.
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch {
		case line == secGit || line == secGem || line == secPath:
			p.mode = modeSource
			p.startSource(line)
		case line == secDeps:
			p.mode = modeDeps
		case line == secPlatforms:
			p.mode = modePlatforms
		case line == secRuby:
			p.mode = modeRuby
		case line == secBundled:
			p.mode = modeBundled
		case len(line) > 0 && line[0] != ' ':
			// An unknown top-level header resets the mode (Bundler ignores it).
			p.mode = modeNone
		default:
			if err := p.feed(line); err != nil {
				return nil, err
			}
		}
	}
	return p.lf, nil
}

var mergeConflictRE = regexp.MustCompile(`<<<<<<<|=======|>>>>>>>|\|\|\|\|\|\|\|`)

type parseMode int

const (
	modeNone parseMode = iota
	modeSource
	modeDeps
	modePlatforms
	modeRuby
	modeBundled
)

type parser struct {
	lf          *Lockfile
	mode        parseMode
	curSrcType  string
	curOpts     map[string][]string
	curSource   *LockSource
	curSpec     *Spec
	specsByFull map[string]*Spec
}

func (p *parser) startSource(header string) {
	p.curSrcType = header
	p.curOpts = map[string][]string{}
	p.curSource = nil
}

func (p *parser) feed(line string) error {
	switch p.mode {
	case modeSource:
		return p.parseSource(line)
	case modeDeps:
		return p.parseDependency(line)
	case modePlatforms:
		p.parsePlatform(line)
	case modeRuby:
		p.lf.RubyVersion = strings.TrimSpace(line)
	case modeBundled:
		v := strings.TrimSpace(line)
		if rubygems.CorrectVersion(v) {
			p.lf.BundledWith = v
		}
	}
	return nil
}

// parseSource handles the lines under a GIT/GEM/PATH header: option lines, the
// "  specs:" line that materializes the source, and spec lines after it.
func (p *parser) parseSource(line string) error {
	if line == "  specs:" {
		p.materializeSource()
		return nil
	}
	if m := optionRE.FindStringSubmatch(line); m != nil && p.curSource == nil {
		key, val := m[1], m[2]
		p.curOpts[key] = append(p.curOpts[key], val)
		return nil
	}
	// Otherwise it is a spec or nested-dependency line.
	return p.parseSpec(line)
}

// materializeSource builds the Source from the collected options.
func (p *parser) materializeSource() {
	src := &Source{}
	first := func(k string) string {
		if vs := p.curOpts[k]; len(vs) > 0 {
			return vs[0]
		}
		return ""
	}
	switch p.curSrcType {
	case secGem:
		src.Type = GemSource
		// remotes appear in reverse on emit, so the first listed in the file is
		// the last remote; reverse them back to internal order.
		rem := p.curOpts["remote"]
		src.Remotes = make([]string, len(rem))
		for i := range rem {
			src.Remotes[len(rem)-1-i] = rem[i]
		}
	case secPath:
		src.Type = PathSource
		src.Remotes = []string{first("remote")}
		src.Glob = first("glob")
	case secGit:
		src.Type = GitSource
		src.Remotes = []string{first("remote")}
		src.Revision = first("revision")
		src.Ref = first("ref")
		src.Branch = first("branch")
		src.Tag = first("tag")
		src.Submodules = first("submodules")
		src.Glob = first("glob")
	}
	ls := &LockSource{Source: src}
	p.curSource = ls
	p.lf.Sources = append(p.lf.Sources, ls)
}

func (p *parser) parseSpec(line string) error {
	// The caller only routes non-blank, space-indented lines here, and the
	// regex's name/version groups are all optional, so it always matches.
	m := nameVersionRE.FindStringSubmatch(line)
	spaces := m[1]
	name := m[2]
	verStr := m[3]
	platform := m[4]

	switch len(spaces) {
	case 4:
		v, err := rubygems.NewVersion(verStr)
		if err != nil {
			return &ParseError{fmt.Sprintf("bad version %q for %q: %v", verStr, name, err)}
		}
		spec := &Spec{Name: name, Version: v, Platform: platform, Source: p.curSource.Source}
		p.curSpec = spec
		p.curSource.Specs = append(p.curSource.Specs, spec)
		p.specsByFull[spec.fullName()] = spec
	case 6:
		if p.curSpec == nil {
			return nil
		}
		var req *rubygems.Requirement
		if verStr == "" {
			req = rubygems.DefaultRequirement()
		} else {
			parts := strings.Split(verStr, ",")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
			r, err := rubygems.NewRequirement(parts...)
			if err != nil {
				return &ParseError{fmt.Sprintf("bad requirement %q for %q: %v", verStr, name, err)}
			}
			req = r
		}
		p.curSpec.Dependencies = append(p.curSpec.Dependencies, SpecDependency{Name: name, Requirement: req})
	}
	return nil
}

func (p *parser) parseDependency(line string) error {
	m := nameVersionRE.FindStringSubmatch(line)
	if len(m[1]) != 2 { // dependencies are at exactly 2 spaces
		return nil
	}
	name := m[2]
	verStr := m[3]
	pinned := m[5] == "!"

	var req *rubygems.Requirement
	if verStr == "" {
		req = rubygems.DefaultRequirement()
	} else {
		parts := strings.Split(verStr, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		r, err := rubygems.NewRequirement(parts...)
		if err != nil {
			return &ParseError{fmt.Sprintf("bad requirement %q for %q: %v", verStr, name, err)}
		}
		req = r
	}
	dep := &Dependency{Name: name, Requirement: req}
	if pinned && name != "bundler" {
		// Bundler attaches the dep's pinned source from the matching spec; do the
		// same so the "!" round-trips. If no matching spec, leave a sentinel
		// non-GEM source so pinned() stays true.
		if spec := p.findSpecByName(name); spec != nil {
			dep.Source = spec.Source
		} else {
			dep.Source = &Source{Type: PathSource}
		}
	}
	p.lf.Dependencies = append(p.lf.Dependencies, dep)
	return nil
}

func (p *parser) findSpecByName(name string) *Spec {
	for _, ls := range p.lf.Sources {
		for _, s := range ls.Specs {
			if s.Name == name {
				return s
			}
		}
	}
	return nil
}

func (p *parser) parsePlatform(line string) {
	if strings.HasPrefix(line, "  ") {
		p.lf.Platforms = append(p.lf.Platforms, strings.TrimSpace(line[2:]))
	}
}
