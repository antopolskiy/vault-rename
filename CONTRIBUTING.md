# Contributing

Contributions should preserve the project's central invariant: a rename either
commits exactly the validated plan or leaves the vault unchanged without
overwriting newer external edits.

Before opening a pull request:

```bash
just check
just test-e2e
just public-audit
```

Changes to parsing, planning, transactions, or recovery should include
byte-for-byte tests and failure cases. Prefer exact byte patches over document
serialization, and fail closed when a reference cannot be resolved safely.

Do not contribute raw notes, vault exports, local absolute paths, contact
details, credentials, or other private data. Test fixtures must use fabricated
content and reserved domains such as `example.invalid`. Preserve only the
minimum structural shape needed to reproduce an edge case.

Security-sensitive reports should follow [SECURITY.md](SECURITY.md), not a
public issue.
