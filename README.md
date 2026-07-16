# vault-rename

`vault-rename` is an agent-oriented safety boundary for renaming files in an
Obsidian vault. An agent or automation chooses the new semantic name; this tool
validates the name, repairs references it can prove are safe to change, records
an audit trail, and recovers from interrupted operations.

It is designed for vault-management workflows where an agent needs deterministic
tooling rather than unrestricted filesystem access. The command provides:

- enforceable naming standards for notes and attachments;
- exact-range backlink repair without broad text replacement;
- dry-run planning and stable JSON output for agent orchestration;
- a separate SQLite paper trail for rename operations;
- per-vault locking, durable recovery journals, and conflict-safe rollback; and
- fail-closed behavior for ambiguous links, unsafe paths, collisions, symlinks,
  hard links, and unsupported structured references.

The tool renames one regular file at a time and never changes its parent
directory or final extension. Moving a file is intentionally a separate
operation.

## Agent workflow

A typical vault-management agent should:

1. inspect the file and derive a concise, descriptive name from its contents;
2. call `vault-rename` to validate and apply that name;
3. inspect the machine-readable result; and
4. route the file with a separate move command that preserves the new basename.

The agent remains responsible for semantic judgment. `vault-rename` does not
invent filenames or decide whether a title accurately describes a document.

Mutating calls require a reason so every completed or rolled-back operation has
useful provenance:

```bash
./bin/vault-rename \
  --root /path/to/vault \
  --reason "Normalize filename before routing" \
  --actor vault-agent \
  "inbox/old-name.md" \
  "Descriptive note title.md"
```

Use `--dry-run` for a validated, read-only plan and `--json` for stable
machine-readable output:

```bash
./bin/vault-rename \
  --root /path/to/vault \
  --dry-run \
  --json \
  "inbox/old-name.md" \
  "Descriptive note title.md"
```

## Naming contract

Markdown files use human-readable note titles, such as
`Descriptive note title.md`. Non-Markdown files use lowercase Unicode kebab
case, such as `20260704-reference-document.pdf`.

The validator rejects path components, extension changes, generic names,
identifier-only names, capture-device filenames, irregular whitespace,
platform-reserved names, and canonical Unicode or case-folding collisions. It
supports case-only renames safely through an internal temporary filename.

## Reference repair

The default `repair` mode rewrites only byte ranges belonging to references that
resolve conclusively to the renamed file. Supported forms include wikilinks,
embeds, aliases, fragments, Markdown links, relative paths, URL-encoded paths,
selected frontmatter fields, and self-links.

Code spans, fenced code, HTML comments, external URLs, visible link labels, and
ordinary prose are preserved. Recognized structured formats that cannot yet be
rewritten cause a safety error by default.

Backlink behavior can be changed per invocation:

```text
--backlinks repair   Repair proven references (default)
--backlinks check    Reject the rename if references would need changes
--backlinks off      Rename without rewriting, while still reporting references
```

## Build

The production binary is CGO-free and has no runtime dependency on this source
tree:

```bash
just build
./bin/vault-rename --version
```

Without `just`:

```bash
CGO_ENABLED=0 go build -trimpath -o bin/vault-rename ./cmd/vault-rename
```

The project currently targets Go 1.25.

## Configuration

An optional strict TOML file named `.vault-rename.toml` may be placed at the
vault root:

```toml
version = 1

backlinks = "repair"
unsupported_references = "error"
frontmatter_title = "exact-match"

log_path = ""
recovery_path = ""
```

Unknown keys and unsupported versions are rejected. Relative state paths are
resolved against the vault root. Empty paths use the platform user-state
directory under `vault-rename/vaults/<vault-id>/`.

Recovery is mandatory. A dry run does not create state directories, audit rows,
or recovery files.

## Audit and recovery

Rename operations use their own SQLite ledger. The database records operation
metadata, affected paths, hashes, patch counts, reference changes, actor,
reason, status, and optional batch identifiers. It does not store vault file
contents.

Before modifying the vault, the tool writes exact backups and a durable recovery
manifest. If a failure occurs, changes are rolled back in reverse order. A newer
external edit is never overwritten during recovery; the operation stops with a
recovery conflict and retains the recovery data for inspection.

## Development

```bash
just test
just test-race
just test-e2e
just public-audit
just coverage
just lint
just check
```

Representative fixtures under `testdata/representative-vault` preserve
important Obsidian structures while using fabricated names and content. The
fixture audit rejects private-looking paths, contact details, credential
patterns, and non-reserved external URLs.

The optional live-vault corpus test is parser-only and does not invoke the
transaction layer:

```bash
VAULT_RENAME_LIVE_VAULT=/path/to/vault just test-live-vault
```

Never contribute raw vault files. See [CONTRIBUTING.md](CONTRIBUTING.md) and
[SECURITY.md](SECURITY.md).

## License

MIT. See [LICENSE](LICENSE).
