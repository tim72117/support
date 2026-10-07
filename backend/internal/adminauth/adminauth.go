// Package adminauth decides which already-authenticated users are allowed
// to use the platform-admin back office (internal/admin).
//
// Deliberately NOT a separate account system: there is no admin login form,
// no admin password, and no admin accounts table. An admin is simply a
// normal business-owner account (internal/session) whose email appears in
// the ADMIN_EMAILS allowlist read from the environment at startup. This is
// intentionally simpler than (and should not be confused with) tripace's
// adminserver, which bootstraps a wholly separate admin identity with its
// own bootstrap credentials and session cookie — ai-support has exactly one
// login flow (/auth/login) and one session cookie (internal/session), and
// admin-ness is just an extra fact checked about whoever that session
// belongs to.
package adminauth

import "strings"

// Allowlist is the set of emails (compared case-insensitively) permitted to
// use the admin API, built once at startup from ADMIN_EMAILS.
type Allowlist struct {
	emails map[string]bool
}

// New builds an Allowlist from a comma-separated list of emails, as read
// from the ADMIN_EMAILS environment variable. Blank entries are dropped;
// comparison is case-insensitive and ignores surrounding whitespace, the
// same normalization internal/session applies to a login email, so
// "Tim@Example.com" in ADMIN_EMAILS matches an account that registered as
// "tim@example.com".
func New(raw string) *Allowlist {
	emails := make(map[string]bool)
	for _, e := range strings.Split(raw, ",") {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" {
			emails[e] = true
		}
	}
	return &Allowlist{emails: emails}
}

// IsAdmin reports whether email is in the allowlist.
func (a *Allowlist) IsAdmin(email string) bool {
	if a == nil {
		return false
	}
	return a.emails[strings.ToLower(strings.TrimSpace(email))]
}

// Empty reports whether no admin emails are configured at all — used only
// to log a single startup warning (an empty ADMIN_EMAILS means the admin
// API is mounted but unusable by anyone, which is almost certainly a
// misconfiguration rather than intended).
func (a *Allowlist) Empty() bool {
	return a == nil || len(a.emails) == 0
}
