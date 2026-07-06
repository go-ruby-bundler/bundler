<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-bundler/brand/main/social/go-ruby-bundler-bundler.png" alt="go-ruby-bundler/bundler" width="720"></p>

# bundler — go-ruby-bundler

[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)
[![CGO](https://img.shields.io/badge/cgo-0-1a7f37)](#)

**A pure-Go (no cgo) reimplementation of the deterministic, pure-compute core of
Ruby's [Bundler](https://bundler.io)** — the `bundler` gem's Gemfile DSL reader,
its `Gemfile.lock` codec, the `Gem::Version` / `Gem::Requirement` algebra, and the
backtracking dependency resolver — **without any Ruby runtime**.

It is byte-for-byte faithful to MRI's Bundler (lockfile target 2.6.x) on the
things that matter for a deterministic, reproducible build: a real `Gemfile.lock`
round-trips identically, version/requirement comparisons match `Gem::Version`,
and the resolver activates the same name → version set Bundler does over the same
index. Network gem-fetching is left as a host-side seam (an injected `Index`); no
network, no filesystem installs.

It builds on [go-ruby-rubygems](https://github.com/go-ruby-rubygems/rubygems) for
the version algebra, so the whole stack is `CGO=0`. It is the dependency-resolution
backend for [go-embedded-ruby](https://github.com/go-embedded-ruby/ruby)
(`rbgo`), a sibling of the other `go-ruby-*` reimplementations
([yaml](https://github.com/go-ruby-yaml/yaml),
[regexp](https://github.com/go-ruby-regexp/regexp),
[marshal](https://github.com/go-ruby-marshal/marshal), …).

## What's in scope

| Area | API | Notes |
| --- | --- | --- |
| **Gemfile DSL** | `ParseGemfile(string) (*Gemfile, error)` | `source` / `ruby` / `gem "n", "req", opts` / `group do…end` / `gemspec` / `git_source`. Options: `:group(s)`, `:require`, `:platform(s)`, `:git`, `:path`, `:branch`, `:ref`, `:tag`, `:submodules`, `:source`. Dynamic-Ruby forms are reported as `*GemfileError`, never dropped. |
| **Lockfile codec** | `ParseLockfile(string) (*Lockfile, error)`, `(*Lockfile).String()` / `.Bytes()` | GEM / GIT / PATH sources, nested `specs:` deps, `PLATFORMS`, `DEPENDENCIES` (with `!` pins), `RUBY VERSION`, `BUNDLED WITH`. Byte-for-byte round-trip. |
| **Version / requirement** | (re-exported via `go-ruby-rubygems`) | `Gem::Version` (incl. prerelease ordering), `Gem::Requirement` (`~>`, `>=`, `<`, `=`, compound). |
| **Resolver** | `Resolve(deps, Index, *Source) (*Resolution, error)` | Backtracking + activation + conflict resolution; returns a `*VersionConflict` Bundler-style on an unsatisfiable graph. |
| **Definition glue** | `(*Definition).Resolve(Index)`, `.Lockfile(*Resolution)` | Resolve → serialize, grouping specs by source. |

## What's a host-side seam

- **Fetching the gem index** from rubygems.org / a compact-index mirror. Supply
  an `Index` (HTTP, a local mirror, or a test fixture — `MapIndex` is provided).
- **Downloading / unpacking / installing** `.gem` files and the `bundle install`
  filesystem writes.
- **Evaluating a *dynamic* Gemfile** (computed names, `install_if`, scoped
  `source` blocks). `ParseGemfile` reads the static forms; the host evaluates the
  rest to a `[]*Dependency`.

## Usage

```go
gf, err := bundler.ParseGemfile(`
source "https://rubygems.org"
gem "rake", "~> 13.0"
gem "rspec", ">= 3.0", "< 4.0"
`)
if err != nil {
    log.Fatal(err)
}

// idx is your Index implementation (network, mirror, fixture).
src := &bundler.Source{Type: bundler.GemSource, Remotes: []string{"https://rubygems.org/"}}
res, err := bundler.Resolve(gf.Dependencies(), idx, src)
if err != nil {
    log.Fatal(err) // *bundler.VersionConflict on an unsatisfiable graph
}

def := &bundler.Definition{
    Dependencies: gf.Dependencies(),
    Platforms:    []string{"ruby"},
    BundledWith:  "2.6.9",
    Source:       src,
}
os.WriteFile("Gemfile.lock", def.Lockfile(res).Bytes(), 0o644)
```

## Tests & coverage

100% statement coverage, enforced in CI, with a differential **MRI oracle**: the
tests drive the real `bundle` binary against a local offline gem repo and assert
our resolver activates the same set and our serializer reproduces Bundler's
`Gemfile.lock` byte-for-byte; a second oracle drives `Bundler::Dsl.evaluate` and
checks `ParseGemfile` extracts the same gem model. The oracle tests skip
themselves where `ruby`/`bundle` are absent (Windows, the cross-arch qemu lanes),
so the deterministic ruby-free suite alone holds the 100% gate everywhere.

```sh
GOWORK=off go test -race -cover ./...
```

Validated on the six supported 64-bit Go targets (amd64, arm64, riscv64, loong64,
ppc64le, s390x) and three OSes (Linux, macOS, Windows).

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright (c) the go-ruby-bundler/bundler
authors.

## WebAssembly

Being pure Go (CGO=0), this library also compiles to **WebAssembly** — both
`GOOS=js GOARCH=wasm` (browser / Node.js) and `GOOS=wasip1 GOARCH=wasm` (WASI).
CI builds both targets on every push, alongside the six 64-bit native/qemu arches.

```sh
GOOS=js     GOARCH=wasm go build ./...   # browser / Node
GOOS=wasip1 GOARCH=wasm go build ./...   # WASI (wasmtime, wasmer, wasmedge, …)
```
