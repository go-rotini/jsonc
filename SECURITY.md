# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly by emailing **matthewcgetz@gmail.com**. Do not open a public issue.

You should receive a response within 72 hours. If accepted, a fix will be developed privately and released as a patch version.

## Resource Limits

This package defaults to safe behavior to mitigate denial-of-service attacks:

- **Nesting depth** is bounded to prevent stack exhaustion.
- **Document size** can be capped via `WithMaxDocumentSize` option.
- **Key count** is bounded via `WithMaxKeys` to limit the total number of keys in a document.
- **Node count** is bounded via `WithMaxNodes` to limit AST size.

These limits can be configured via decode options but are set to safe defaults out of the box.
