# Changelog

All notable changes to this extension will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-07-07

### Added

- Initial release of the EARS Requirements Syntax extension.
- `speckit.ears.author` — draft requirements for a feature directly in EARS format, classified by pattern with stable requirement IDs.
- `speckit.ears.lint` — audit existing requirements for EARS conformance and ambiguity, producing a read-only lint report with suggested rewrites.
- `speckit.ears.convert` — rewrite free-form requirements into EARS patterns and produce a traceability matrix from originals to EARS statements.
