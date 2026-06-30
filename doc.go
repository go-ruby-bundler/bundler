// Copyright (c) the go-ruby-bundler/bundler authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package bundler is a pure-Go (no cgo) reimplementation of the pure-compute
// core of Ruby's Bundler — the dependency resolver and the Gemfile.lock codec.
//
// It is byte-for-byte faithful to MRI's Bundler (target 2.6.x) on the two
// things that matter most for a deterministic, reproducible build:
//
//   - LockfileParser / lock serialization: parse a real Gemfile.lock into the
//     spec set, and re-emit the exact same bytes (the canonical full-name
//     ordering, the 2-space spec indent, the 3-space "BUNDLED WITH" line, the
//     descending requirement order, the "!" pinned-dependency markers, and the
//     blank lines between sections). A real lock round-trips identically.
//
//   - The resolver: a backtracking dependency resolver (the same shape of
//     activation + conflict-resolution that Bundler's Molinillo/PubGrub solver
//     produces) over an injected index. Given a set of root dependencies and an
//     Index (name -> available versions, each with its own dependencies),
//     it resolves to a consistent SpecSet, or returns a VersionConflict whose
//     message explains the conflict the way Bundler does.
//
// It builds on github.com/go-ruby-rubygems/rubygems for the version algebra
// (Gem::Version comparison, Gem::Requirement satisfaction including the
// pessimistic "~>" bound math, and Gem::Dependency), so the whole stack is
// CGO=0.
//
// # In scope (pure compute)
//
//   - Gemfile DSL reader ([ParseGemfile]): the canonical, deterministic forms
//     of a Gemfile — source, ruby, gem "name", "req", options
//     (:group/:groups/:require/:platform(s)/:git/:path/:branch/:ref/:tag/
//     :submodules/:source), group do … end blocks, gemspec, and a custom
//     git_source(:name){ |repo| … } template. Arbitrary-Ruby forms (a computed
//     name, an install_if predicate, a scoped source block) are reported as a
//     *GemfileError naming the line, never silently dropped.
//   - Bundler::LockfileParser:  parse GEM / PATH / GIT / PLATFORMS /
//     DEPENDENCIES / RUBY VERSION / BUNDLED WITH sections; the remote:/specs:
//     tree; the "gem (ver)" + nested-dependency indentation; the "!" pinned
//     markers. Plus the inverse: serialize a resolution back to lockfile bytes.
//   - The resolver: backtracking + activation + conflict resolution + the
//     deterministic sorting that fixes Bundler's output, over an injected Index.
//   - Bundler::Dependency:      gem name + requirement + groups + platforms +
//     source.
//   - Bundler::Definition (pure parts): the glue holding sources, dependencies
//     and a locked SpecSet, and the lock-string production.
//
// # Out of scope (host-side seams)
//
//   - Fetching the gem index from a remote source (the rubygems.org API or the
//     compact index). The resolver takes an injected [Index] instead — supply
//     one however you like (HTTP, a local mirror, a test fixture). No network.
//   - Downloading, unpacking and installing .gem files; the `bundle install`
//     filesystem writes; require-time activation.
//   - Fully evaluating a *dynamic* Gemfile. [ParseGemfile] reads the canonical
//     static forms directly (no interpreter); a Gemfile that computes gem names
//     or requirements at runtime, or uses install_if/env/scoped-source blocks,
//     is reported as a *GemfileError so the host (a Ruby evaluator such as rbgo)
//     can evaluate it and hand this library the resulting []*Dependency instead.
//
// Downstream, rbgo (the pure-Go Ruby) drives this library: for a static Gemfile
// it calls [ParseGemfile]; for a dynamic one it evaluates the Ruby to a
// []*Dependency; either way it fetches an index from the network into an Index,
// calls [Resolve], and writes the SpecSet back out with [Lockfile.Bytes].
package bundler
