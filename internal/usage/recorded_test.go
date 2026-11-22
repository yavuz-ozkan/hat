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
)

// statusLineInput is shaped like what Claude Code 2.1.330 sends its status
// line, plus the optional windows its structured usage data can carry and a
// key hat doesn't know.
const statusLineInput = `{
  "session_id": "5f1c",
  "cwd": "/home/someone/myapp",
  "model": {"id": "claude-opus-4-5", "display_name": "Opus"},
  "workspace": {"current_dir": "/home/someone/myapp", "project_dir": "/home/someone/myapp", "added_dirs": []},
  "version": "2.1.330",
  "cost": {"total_cost_usd": 0.42, "total_duration_ms": 61000},
  "context_window": {"used_percentage": 12, "context_window_size": 200000},
  "rate_limits": {
    "five_hour": {"used_percentage": 23.4, "resets_at": 1795680000},
    "seven_day": {"used_percentage": 41, "resets_at": 1795600000},
    "seven_day_opus": {"used_percentage": 12.5, "resets_at": "2026-11-27T09:00:00Z"},
    "seven_day_sonnet": null,
    "spend_limit": {"used_percentage": 3, "resets_at": 1795600000},
    "model_scoped": [
      {"display_name": "Fable", "utilization": 64, "resets_at": "2026-11-27T09:00:00Z"},
      {"display_name": "Haiku", "utilization": null, "resets_at": null}
    ]
  }
}`

func TestLimitsFromTheStatusLineShape(t *testing.T) {
	line, err := ParseStatusLine([]byte(statusLineInput))
	if err != nil {
		t.Fatal(err)
	}

	limits, err := LimitsFrom(line.RateLimits)
	if err != nil {
		t.Fatal(err)
	}

	if line.Model.DisplayName != "Opus" || len(limits) != 4 {
		t.Fatalf("model %q, limits %+v", line.Model.DisplayName, limits)
	}
	fiveHour := limits[0]
	if fiveHour.Kind != "five_hour" || fiveHour.Label != "5-hour" || fiveHour.Percent != 23.4 || fiveHour.ResetsAt == nil || fiveHour.ResetsAt.Unix() != 1795680000 {
		t.Errorf("five_hour = %+v", fiveHour)
	}
	if week := limits[1]; week.Kind != "seven_day" || week.Label != "Week" || week.Percent != 41 {
		t.Errorf("seven_day = %+v", week)
	}
	if opus := limits[2]; opus.Model != "Opus" || opus.ResetsAt == nil || !opus.ResetsAt.Equal(time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("seven_day_opus with an ISO reset = %+v", opus)
	}
	if fable := limits[3]; fable.Kind != "model_scoped" || fable.Model != "Fable" || fable.Percent != 64 {
		t.Errorf("model_scoped = %+v", fable)
	}
}

func TestParseStatusLineToleratesMissingFields(t *testing.T) {
	line, err := ParseStatusLine([]byte(`{"model": {}}`))
	if err != nil {
		t.Fatal(err)
	}
	if line.hasRateLimits() || DefaultLine(line) != "" {
		t.Errorf("line = %+v, default %q", line, DefaultLine(line))
	}
}

func TestDefaultLine(t *testing.T) {
	line, _ := ParseStatusLine([]byte(statusLineInput))
	if got, want := DefaultLine(line), "Opus · 5h 23% · week 41%"; got != want {
		t.Errorf("DefaultLine = %q, want %q", got, want)
	}
	noLimits, _ := ParseStatusLine([]byte(`{"model": {"display_name": "Sonnet"}}`))
	if got := DefaultLine(noLimits); got != "Sonnet" {
		t.Errorf("DefaultLine without limits = %q", got)
	}
}

func TestSaveKeepsTheLastRecordWhenALineHasNoLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage", "main.json")
	withLimits, _ := ParseStatusLine([]byte(statusLineInput))
	freshSession, _ := ParseStatusLine([]byte(`{"model": {"display_name": "Opus"}}`))
	explicitNull, _ := ParseStatusLine([]byte(`{"rate_limits": null}`))

	if err := Save(path, withLimits, now); err != nil {
		t.Fatal(err)
	}
	for _, line := range []StatusLine{freshSession, explicitNull} {
		if err := Save(path, line, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	record, err := Load(path)
	if err != nil || !record.RecordedAt.Equal(now) {
		t.Fatalf("record = %+v, %v; want the one saved at %s", record, err, now)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("left temporary files: %v", entries)
	}
}

func TestRecordedShowsTheSavedLimitsAndWhenTheyWereSeen(t *testing.T) {
	acct := accountWithSignIn(t, "good-token", now.Add(time.Hour))
	path := filepath.Join(t.TempDir(), "claude-work.json")
	line, _ := ParseStatusLine([]byte(statusLineInput))
	if err := Save(path, line, now.Add(-12*time.Minute)); err != nil {
		t.Fatal(err)
	}

	row := Recorded(acct, path, now)

	if row.Problem != nil || len(row.Limits) != 4 || row.Email != "work@example.com" {
		t.Fatalf("row = %+v", row)
	}
	if row.AsOf == nil || !row.AsOf.Equal(now.Add(-12*time.Minute)) {
		t.Errorf("asOf = %v", row.AsOf)
	}
}

func TestRecordedLeavesOutWindowsThatHaveResetSince(t *testing.T) {
	acct := accountWithSignIn(t, "good-token", now.Add(time.Hour))
	path := filepath.Join(t.TempDir(), "claude-work.json")
	line, _ := ParseStatusLine([]byte(`{"rate_limits": {
	  "five_hour": {"used_percentage": 95, "resets_at": ` + fmt.Sprint(now.Add(-time.Hour).Unix()) + `},
	  "seven_day": {"used_percentage": 41, "resets_at": ` + fmt.Sprint(now.Add(48*time.Hour).Unix()) + `}}}`))
	if err := Save(path, line, now.Add(-5*time.Hour)); err != nil {
		t.Fatal(err)
	}

	row := Recorded(acct, path, now)

	if len(row.Limits) != 1 || row.Limits[0].Kind != "seven_day" {
		t.Errorf("limits = %+v, want only the week that hasn't reset", row.Limits)
	}
}

func TestRecordedWithNothingRecordedSaysHowToStart(t *testing.T) {
	signedIn := accountWithSignIn(t, "good-token", now.Add(time.Hour))
	neverSignedIn := accountWithSignIn(t, "", time.Time{})
	missing := filepath.Join(t.TempDir(), "none.json")

	row := Recorded(signedIn, missing, now)
	if row.Problem == nil || row.Problem.Code != NoUsageRecorded || !strings.Contains(row.Problem.Message, `"hat statusline"`) || row.Limits == nil {
		t.Errorf("signed in, nothing recorded: %+v", row)
	}
	row = Recorded(neverSignedIn, missing, now)
	if row.Problem == nil || row.Problem.Code != NotSignedIn {
		t.Errorf("never signed in: %+v", row.Problem)
	}
}

func TestRecordedReportsADamagedRecord(t *testing.T) {
	acct := accountWithSignIn(t, "good-token", now.Add(time.Hour))
	path := filepath.Join(t.TempDir(), "claude-work.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	row := Recorded(acct, path, now)

	if row.Problem == nil || row.Problem.Code != NoUsageRecorded || !strings.Contains(row.Problem.Message, "rewritten") {
		t.Errorf("problem = %+v", row.Problem)
	}
}

func TestRenderSaysHowOldTheNumbersAre(t *testing.T) {
	ages := map[time.Duration]string{
		20 * time.Second: "as of just now",
		12 * time.Minute: "as of 12m ago",
		5 * time.Hour:    "as of 5h ago",
		72 * time.Hour:   "as of 3d ago",
	}
	for age, want := range ages {
		asOf := now.Add(-age)
		var out bytes.Buffer
		Render(&out, []Row{{Account: "work", Email: "work@example.com", Limits: []Limit{{Label: "5-hour", Percent: 5}}, AsOf: &asOf}}, Style{}, now)
		if !strings.Contains(out.String(), want) {
			t.Errorf("age %s: output missing %q:\n%s", age, want, out.String())
		}
	}
}

func TestLiveClientNeverFollowsARedirect(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the redirect was followed, carrying Authorization %q", r.Header.Get("Authorization"))
	}))
	t.Cleanup(elsewhere.Close)
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirecting.Close)
	acct := accountWithSignIn(t, "good-token", now.Add(time.Hour))

	row := Checker{Client: LiveClient(), Endpoint: redirecting.URL, Now: func() time.Time { return now }}.Measure(acct)

	if row.Problem == nil || row.Problem.Code != UnexpectedResponse || !strings.Contains(row.Problem.Message, "HTTP 307") {
		t.Errorf("problem = %+v", row.Problem)
	}
}
