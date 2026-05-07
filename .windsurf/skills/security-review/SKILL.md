---
name: security-review
description: Performs security review on code changes
---

# Security Review Skill

Review code for security vulnerabilities. Every finding must include: file, line, vulnerability class, and a specific remediation. Do not report speculative issues — only findings backed by evidence in the code.

<investigate_before_answering>
Read the actual code before making security claims. Never assume a vulnerability without evidence.
</investigate_before_answering>

<checklist>
1. [ ] No hardcoded secrets, API keys, or credentials — use `os.Getenv` or config loader
2. [ ] No sensitive data in logs (passwords, tokens, PII)
3. [ ] All user input validated before use
4. [ ] Errors do not expose internal paths or stack traces
5. [ ] SQL queries parameterised — no string concatenation
6. [ ] `govulncheck` passing — no known CVEs in dependencies
7. [ ] Ensure compliance with `rules/security.md`
</checklist>
