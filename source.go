// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

package bundler

import "strings"

// SourceType is the kind of a lockfile source section.
type SourceType string

const (
	// GemSource is a rubygems remote ("GEM") section.
	GemSource SourceType = "GEM"
	// PathSource is a local path ("PATH") section.
	PathSource SourceType = "PATH"
	// GitSource is a git ("GIT") section.
	GitSource SourceType = "GIT"
)

// gitDefaultGlob is Bundler's default gemspec glob for a git source; it is
// omitted from the lockfile when unchanged (Source::Git#default_glob?).
const gitDefaultGlob = "{,*,*/*}.gemspec"

// pathDefaultGlob is Bundler's default gemspec glob for a path source; it is
// omitted from the lockfile when unchanged (Source::Path::DEFAULT_GLOB).
const pathDefaultGlob = "{,*,*/*}.gemspec"

// Source describes one resolution source — a rubygems remote, a local path or a
// git checkout — exactly as it appears in a Gemfile.lock section header. It is
// the pure-data model; fetching from it is a host-side seam.
type Source struct {
	Type SourceType

	// Remotes holds the source's remote URLs. For a GEM source there may be
	// several; for PATH/GIT there is exactly one.
	Remotes []string

	// Revision is the locked git commit (GIT only).
	Revision string
	// Ref, Branch, Tag and Submodules are the optional git locators (GIT only),
	// serialized in this fixed order after revision.
	Ref        string
	Branch     string
	Tag        string
	Submodules string
	// Glob is the gemspec glob (PATH and GIT). The empty string means "the
	// default", which is omitted from the lockfile.
	Glob string
}

// GemRemote is the single remote of a GEM source (or "" if none); a convenience
// for the common case.
func (s *Source) GemRemote() string {
	if len(s.Remotes) == 0 {
		return ""
	}
	return s.Remotes[0]
}

// header renders the source's lockfile section header (everything up to and
// including the "  specs:\n" line), byte-for-byte matching Bundler's
// Source#to_lock.
func (s *Source) header() string {
	var b strings.Builder
	switch s.Type {
	case GemSource:
		b.WriteString("GEM\n")
		// Bundler emits remotes in reverse order (lockfile_remotes.reverse_each).
		for i := len(s.Remotes) - 1; i >= 0; i-- {
			b.WriteString("  remote: " + s.Remotes[i] + "\n")
		}
		b.WriteString("  specs:\n")
	case PathSource:
		b.WriteString("PATH\n")
		b.WriteString("  remote: " + s.GemRemote() + "\n")
		if s.Glob != "" && s.Glob != pathDefaultGlob {
			b.WriteString("  glob: " + s.Glob + "\n")
		}
		b.WriteString("  specs:\n")
	case GitSource:
		b.WriteString("GIT\n")
		b.WriteString("  remote: " + s.GemRemote() + "\n")
		b.WriteString("  revision: " + s.Revision + "\n")
		// Bundler order: ref, branch, tag, submodules, then glob.
		if s.Ref != "" {
			b.WriteString("  ref: " + s.Ref + "\n")
		}
		if s.Branch != "" {
			b.WriteString("  branch: " + s.Branch + "\n")
		}
		if s.Tag != "" {
			b.WriteString("  tag: " + s.Tag + "\n")
		}
		if s.Submodules != "" {
			b.WriteString("  submodules: " + s.Submodules + "\n")
		}
		if s.Glob != "" && s.Glob != gitDefaultGlob {
			b.WriteString("  glob: " + s.Glob + "\n")
		}
		b.WriteString("  specs:\n")
	}
	return b.String()
}

// pinned reports whether a dependency resolved against this source is pinned
// (gets a "!" suffix in DEPENDENCIES). Bundler marks every non-GEM source pinned.
func (s *Source) pinned() bool { return s.Type != GemSource }
