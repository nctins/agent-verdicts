# Security policy

## Reporting a vulnerability

Please do not open a public issue for security problems. Report them
privately through GitHub's
[private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
("Report a vulnerability" under the repository's **Security** tab).

Include the affected version or commit, steps to reproduce, and the impact
you observed. This is a small project maintained on a best-effort basis, so
there is no guaranteed response time. Fixes go into a new version, and the
advisory is published once a fix is available.

## Supported versions

There is no tagged release yet; fixes land on `main`. Once releases exist,
only the latest one receives security fixes.

## Deployment notes

Agent Verdicts is designed to run behind an HTTPS reverse proxy on a trusted
network. Read "Behind a reverse proxy" in
[docs/deployment.md](docs/deployment.md#behind-a-reverse-proxy) before
exposing an instance to the internet, including its note on login lockouts.
