---
name: security_audit
description: Security vulnerability detection methodology
roles: [security, vulnerability, bug_hunter, audit]
---

## Security Audit Checklist

### Authentication & Authorization
- Hardcoded credentials: grep for "password", "secret", "api_key", "token" in source files
- Missing auth checks on API endpoints
- Insecure session management (predictable tokens, no expiry)

### Injection
- SQL injection: string concatenation in database queries
- Command injection: user input passed to shell/exec functions
- XSS: unescaped user input in HTML templates
- Path traversal: user-controlled file paths without sanitization

### Data Exposure
- Sensitive data in logs (passwords, tokens, PII)
- Debug endpoints left enabled in production
- Overly permissive CORS configuration
- Missing encryption for sensitive data at rest

### Dependencies
- Known vulnerable dependencies (check version numbers)
- Unnecessary dependencies that increase attack surface

### Patterns to grep for
```
grep -r "password\|secret\|api_key\|token" --include="*.go" --include="*.py" --include="*.js"
grep -r "exec\|system\|popen\|eval" --include="*.go" --include="*.py"
grep -r "sql\|query\|SELECT\|INSERT" --include="*.go" --include="*.py"
```
