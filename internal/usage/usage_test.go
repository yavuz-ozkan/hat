package usage

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yavuz-ozkan/hat/internal/account"
)

var now = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

const okBody = `{"limits":[
  {"kind":"weekly_all","percent":78,"resets_at":"2026-11-20T11:00:00.771886-08:00","scope":null},
  {"kind":"weekly_scoped","percent":100,"resets_at":"2026-11-20T10:59:59-08:00","scope":{"model":{"display_name":"Fable"}}},
  {"kind":"brand_new_limit","percent":5,"resets_at":null}
],"five_hour":null}`

func accountWithSignIn(t *testing.T, token string, expiresAt time.Time) account.Account {
	t.Helper()
	dir := t.TempDir()
	acct := account.Account{
		Name:            "work",
		Kind:            account.Managed,
		ConfigDir:       dir,
		CredentialsFile: filepath.Join(dir, ".credentials.json"),
		StateFile:       filepath.Join(dir, ".claude.json"),
	}
	if token != "" {
		writeSignIn(t, acct, token, expiresAt)
	}
	state := `{"oauthAccount":{"emailAddress":"work@example.com"}}`
	if err := os.WriteFile(acct.StateFile, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	return acct
}

func writeSignIn(t *testing.T, acct account.Account, token string, expiresAt time.Time) {
	t.Helper()
	creds := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d}}`, token, expiresAt.UnixMilli())
	if err := os.WriteFile(acct.CredentialsFile, []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
}

func server(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" || r.Header.Get("anthropic-beta") != betaFlag {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func checker(srv *httptest.Server) Checker {
	return Checker{Client: srv.Client(), Endpoint: srv.URL, Now: func() time.Time { return now }}
}

func TestMeasureReadsLimitsAndKeepsUnknownKinds(t *testing.T) {
	acct := accountWithSignIn(t, "good-token", now.Add(time.Hour))

	row := checker(server(t, 200, okBody)).Measure(acct)

	if row.Problem != nil {
		t.Fatalf("problem: %+v", row.Problem)
	}
	if row.Email != "work@example.com" || len(row.Limits) != 3 {
		t.Fatalf("row = %+v", row)
	}
	if l := row.Limits[1]; l.Label != "Week" || l.Model != "Fable" || l.Percent != 100 || l.ResetsAt == nil {
		t.Errorf("Fable limit = %+v", l)
	}
	if l := row.Limits[2]; l.Label != "brand_new_limit" || l.ResetsAt != nil {
		t.Errorf("unknown limit = %+v", l)
	}
}

func TestMeasureNotSignedIn(t *testing.T) {
	acct := accountWithSignIn(t, "", time.Time{})

	row := checker(server(t, 200, okBody)).Measure(acct)

	if row.Problem == nil || row.Problem.Code != NotSignedIn || !strings.Contains(row.Problem.Message, "hat work") {
		t.Errorf("problem = %+v", row.Problem)
	}
	if row.Limits == nil {
		t.Error("limits should be an empty list, not null")
	}
}

func TestNeedsRenewal(t *testing.T) {
	cases := []struct {
		name      string
		token     string
		expiresAt time.Time
		want      bool
	}{
		{"expired", "good-token", now.Add(-time.Hour), true},
		{"expires within the minute", "good-token", now.Add(30 * time.Second), true},
		{"fresh", "good-token", now.Add(time.Hour), false},
		{"not signed in", "", time.Time{}, false},
	}
	for _, c := range cases {
		acct := accountWithSignIn(t, c.token, c.expiresAt)
		if got := NeedsRenewal(acct, now); got != c.want {
			t.Errorf("%s: NeedsRenewal = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMeasureReadsSignInRenewedByClaude(t *testing.T) {
	acct := accountWithSignIn(t, "good-token", now.Add(-time.Hour))
	writeSignIn(t, acct, "good-token", now.Add(8*time.Hour))

	row := checker(server(t, 200, okBody)).Measure(acct)

	if row.Problem != nil || len(row.Limits) != 3 {
		t.Errorf("row = %+v", row)
	}
}

func TestMeasureExpiredAndNotRenewed(t *testing.T) {
	acct := accountWithSignIn(t, "good-token", now.Add(-time.Hour))

	row := checker(server(t, 200, okBody)).Measure(acct)

	if row.Problem == nil || row.Problem.Code != SignInExpired {
		t.Errorf("problem = %+v", row.Problem)
	}
}

func TestMeasureMapsHTTPErrorsToCodes(t *testing.T) {
	cases := []struct {
		token  string
		status int
		body   string
		code   string
	}{
		{"bad-token", 200, okBody, SignInRejected},
		{"good-token", 429, okBody, RateLimited},
		{"good-token", 500, okBody, UnexpectedResponse},
		{"good-token", 200, "not json", UnexpectedResponse},
		{"good-token", 200, `{"rate_limits":[]}`, UnexpectedResponse},
		{"good-token", 200, `{"limits":[{"kind":"weekly_all","pct":40}]}`, UnexpectedResponse},
		{"good-token", 200, `{"limits":[{"type":"weekly_all","percent":40}]}`, UnexpectedResponse},
	}
	for _, c := range cases {
		acct := accountWithSignIn(t, c.token, now.Add(time.Hour))
		row := checker(server(t, c.status, c.body)).Measure(acct)
		if row.Problem == nil || row.Problem.Code != c.code {
			t.Errorf("token=%s status=%d body=%q: problem = %+v, want %s", c.token, c.status, c.body, row.Problem, c.code)
		}
	}
}

func TestRenderShowsBarsResetsAndProblems(t *testing.T) {
	resets := now.Add(26 * time.Hour)
	rows := []Row{
		{Account: "main", Email: "main@example.com", Limits: []Limit{{Label: "Week", Model: "Fable", Percent: 100, ResetsAt: &resets}}},
		{Account: "client-a", Limits: []Limit{}, Problem: &Problem{NotSignedIn, "Not signed in. Run: hat client-a"}},
	}
	var out bytes.Buffer

	Render(&out, rows, Style{}, now)

	text := out.String()
	for _, want := range []string{"main@example.com", "Week, Fable", strings.Repeat("█", barWidth), "100%", "resets in 1d 2h", "(no account yet)", "Not signed in. Run: hat client-a"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}
