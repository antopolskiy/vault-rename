# Security policy

`vault-rename` modifies user-owned knowledge bases, so corruption, unintended
rewrites, path traversal, unsafe recovery, and disclosure of vault contents are
treated as security-sensitive issues.

Please report vulnerabilities through GitHub's private vulnerability reporting
flow on the repository's **Security** tab. Do not include real vault files,
personal paths, credentials, or sensitive note content in a public issue.

A useful report includes:

- the affected version or commit;
- the operating system and filesystem;
- a minimal synthetic vault that reproduces the problem;
- the exact command and observed result; and
- whether recovery artifacts remain available.

Use fabricated content and reserved domains such as `example.invalid` in every
reproduction. If the issue may have modified a real vault, stop further rename
operations and retain the recovery directory until the report is assessed.
