// Package onagentclient will call onagent's own console API on behalf of a
// business owner — creating the onagent app that backs a new business, and
// pushing the business's configured content as that app's tool
// definition(s) whenever it changes.
//
// Deliberately unimplemented in this skeleton: onagent's actual console API
// request/response shapes weren't re-verified against onagent's current
// source before writing this scaffold, and guessing at that contract here
// would risk baking in a wrong shape that's easy to mistake for a verified
// one later. Wire this up by reading onagent's backend/internal/console
// (the same API backend/cmd/onagent's CLI talks to) and backend/internal/
// toolschema for the tool-definition shape it expects, then replace this
// file's TODOs with real HTTP calls authenticated the same way the CLI
// does (see onagent's internal/cliauth / internal/usertoken).
package onagentclient

import "errors"

// ErrNotImplemented is returned by every method until this package is
// actually wired up against onagent's console API.
var ErrNotImplemented = errors.New("onagentclient: not implemented yet")

// Client will hold whatever onagent needs to authenticate these calls (an
// onagent-side account/token for this ai-support deployment itself, not any
// individual business owner's credentials).
type Client struct {
	BaseURL string
}

func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL}
}

// CreateApp will create a new onagent app for a business and return its app
// id and a freshly issued API key (for the consumer-facing page's
// @onagent/bridge connection).
func (c *Client) CreateApp(name string) (appID, apiKey string, err error) {
	return "", "", ErrNotImplemented
}

// PushContent will translate a business's freeform content into onagent
// tool definition(s) and push them to appID via onagent's console API.
func (c *Client) PushContent(appID, content string) error {
	return ErrNotImplemented
}
