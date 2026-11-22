package usage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/yavuz-ozkan/hat/internal/account"
	"github.com/yavuz-ozkan/hat/internal/state"
)

// The live check calls the endpoint behind Claude Code's own /usage screen.
// That endpoint isn't documented, so only the `limits` list is read, and a
// limit kind hat doesn't know is shown by its raw name rather than dropped.
const (
	Endpoint    = "https://api.anthropic.com/api/oauth/usage"
	betaFlag    = "oauth-2025-04-20"
	renewMargin = time.Minute
)

// LiveClient never follows a redirect: the request carries the account's
// token, and a redirect would hand it to whatever host the answer names.
func LiveClient() *http.Client {
	return &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type Checker struct {
	Client   *http.Client
	Endpoint string
	Now      func() time.Time
}

var limitLabels = map[string]string{"session": "5-hour", "weekly_all": "Week", "weekly_scoped": "Week"}

func (c Checker) Measure(acct account.Account) Row {
	doc, _ := state.Read(acct.StateFile)
	row := Row{Account: acct.Name, Kind: kindNames[acct.Kind], Email: doc.Email(), Limits: []Limit{}}

	signIn := readSignIn(acct.CredentialsFile)
	switch {
	case signIn.Token == "":
		row.Problem = &Problem{NotSignedIn, "Not signed in. Run: " + acct.SignInHint()}
		return row
	case !signIn.freshAt(c.Now()):
		row.Problem = &Problem{SignInExpired, "Sign-in expired and Claude couldn't renew it. Sign in again: " + acct.SignInHint()}
		return row
	}

	limits, problem := c.fetch(signIn.Token, acct)
	row.Problem = problem
	if limits != nil {
		row.Limits = limits
		asOf := c.Now()
		row.AsOf = &asOf
	}
	return row
}

func (c Checker) fetch(token string, acct account.Account) ([]Limit, *Problem) {
	req, err := http.NewRequest(http.MethodGet, c.Endpoint, nil)
	if err != nil {
		return nil, &Problem{UnexpectedResponse, err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", betaFlag)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, &Problem{Unreachable, "Couldn't reach Anthropic: " + err.Error()}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, &Problem{SignInRejected, "Anthropic turned this sign-in down. Sign in again: " + acct.SignInHint()}
	case http.StatusTooManyRequests:
		return nil, &Problem{RateLimited, "Anthropic is rate limiting usage checks. Try again in a minute."}
	default:
		return nil, &Problem{UnexpectedResponse, fmt.Sprintf("Usage check failed with HTTP %d. Try again; if it keeps failing, Anthropic may have changed the usage endpoint. Plain hat usage doesn't depend on it.", resp.StatusCode)}
	}

	// Kind, limits, and percent are required: if Anthropic renames one, decoding
	// would quietly give an empty list or 0%, which reads as a real answer.
	var body struct {
		Limits *[]struct {
			Kind     string   `json:"kind"`
			Percent  *float64 `json:"percent"`
			ResetsAt *string  `json:"resets_at"`
			Scope    *struct {
				Model *struct {
					DisplayName string `json:"display_name"`
				} `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
	}
	changed := func(detail string) *Problem {
		return &Problem{UnexpectedResponse, "Anthropic's usage answer wasn't in the expected shape (" + detail + "), so the usage endpoint may have changed. Plain hat usage doesn't depend on it."}
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, changed(err.Error())
	}
	if body.Limits == nil {
		return nil, changed("no limits list")
	}
	limits := []Limit{}
	for _, raw := range *body.Limits {
		if raw.Kind == "" || raw.Percent == nil {
			return nil, changed("a limit without a kind or percent")
		}
		limit := Limit{Kind: raw.Kind, Label: limitLabel(raw.Kind), Percent: *raw.Percent}
		if raw.Scope != nil && raw.Scope.Model != nil {
			limit.Model = raw.Scope.Model.DisplayName
		}
		if raw.ResetsAt != nil {
			if t, err := time.Parse(time.RFC3339Nano, *raw.ResetsAt); err == nil {
				limit.ResetsAt = &t
			}
		}
		limits = append(limits, limit)
	}
	return limits, nil
}

func limitLabel(kind string) string {
	if label, ok := limitLabels[kind]; ok {
		return label
	}
	return kind
}

type signIn struct {
	Token     string
	ExpiresAt time.Time
}

func NeedsRenewal(acct account.Account, now time.Time) bool {
	signIn := readSignIn(acct.CredentialsFile)
	return signIn.Token != "" && !signIn.freshAt(now)
}

func (s signIn) freshAt(now time.Time) bool {
	return s.Token != "" && s.ExpiresAt.After(now.Add(renewMargin))
}

func readSignIn(credentialsFile string) signIn {
	data, err := os.ReadFile(credentialsFile)
	if err != nil {
		return signIn{}
	}
	var file struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &file) != nil {
		return signIn{}
	}
	return signIn{Token: file.ClaudeAiOauth.AccessToken, ExpiresAt: time.UnixMilli(file.ClaudeAiOauth.ExpiresAt)}
}
