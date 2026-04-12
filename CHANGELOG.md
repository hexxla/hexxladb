# Changelog (how to use — not a project history)

This file explains **what** a changelog is for, **how** to maintain one when you ship versions, and **example** entries you can copy. **This template does not track its own release history here**; replace or extend the examples below when you fork or tag real releases.

## What it is for

- **Consumers** of your module or binary need a **human-readable** summary of what changed between versions (features, fixes, breaking changes).
- **Semantic versioning** ([semver](https://semver.org/)) pairs with that narrative: **MAJOR** for incompatible API/behavior, **MINOR** for additive changes, **PATCH** for fixes.
- A changelog complements **`git log`**: it is curated, grouped by impact, and safe to read without spelunking commits.

## Common format: Keep a Changelog

The widely used **[Keep a Changelog](https://keepachangelog.com/)** convention uses:

- An **`[Unreleased]`** section for work not yet tagged.
- **Versioned sections** with a date, newest first.
- **Categories** such as `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, `Security`.

Workflow:

1. During development, add bullets under **`[Unreleased]`** (or collect them in PR descriptions and fold in before release).
2. When you tag **`v1.2.0`**, rename **`[Unreleased]`** to **`[1.2.0] - YYYY-MM-DD`**, add a new empty **`[Unreleased]`** at the top, and publish the tag (and optionally **GitHub Releases**).

## Alternative: GitHub Releases only

Some teams skip **`CHANGELOG.md`** and publish release notes on the **GitHub Releases** page for each tag. That is fine if notes are discoverable and kept in sync with tags. You can still mirror the same categories in the release body.

## Example: `CHANGELOG.md` body (fictional)

The following is **illustrative only** — not this repo’s history.

```markdown
# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Optional Redis-backed cache for session store (behind `REDIS_URL`).

### Fixed

- Correct idle timeout handling when `HTTP_IDLE_TIMEOUT` is unset.

## [1.0.0] - 2026-04-01

### Added

- Initial public HTTP API: `/health`, `/v1/hash`, `/v1/store`, `/v1/messages`.
- Configurable listen address and HTTP timeouts via environment variables.

[Unreleased]: https://github.com/your-org/your-repo/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/your-org/your-repo/releases/tag/v1.0.0
```

## Example: tagging a release (local Git)

```bash
# After updating CHANGELOG.md and merging to main:
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin main --tags
```

Use GitHub’s “Draft a new release” UI to attach binaries or notes if you do not push tags from the CLI.

## Optional: GoReleaser

For **cross-compiled binaries** and release artifacts, [GoReleaser](https://goreleaser.com/) can build from your tag and upload to GitHub Releases. Add a **`goreleaser`** config only when you need that workflow; this template does not include one by default.
