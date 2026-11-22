package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yavuz-ozkan/hat/internal/account"
	"github.com/yavuz-ozkan/hat/internal/state"
	"github.com/yavuz-ozkan/hat/internal/terminal"
)

// StatusLine is the part of the JSON Claude Code sends its status line that
// hat reads. Claude Code adds rate_limits only once it has seen the account's
// limits, and leaves it out before that (Claude Code 2.1.330).
type StatusLine struct {
	Model struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
	RateLimits json.RawMessage `json:"rate_limits"`
}

func ParseStatusLine(input []byte) (StatusLine, error) {
	var line StatusLine
	err := json.Unmarshal(input, &line)
	return line, err
}

func (s StatusLine) hasRateLimits() bool {
	trimmed := bytes.TrimSpace(s.RateLimits)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

type Record struct {
	RecordedAt time.Time       `json:"recordedAt"`
	RateLimits json.RawMessage `json:"rateLimits"`
}

// Save records line's limits in path. A status line without limits leaves the
// last record alone: a new session renders before its first answer from
// Anthropic, and that must not erase what the previous session saw.
func Save(path string, line StatusLine, now time.Time) error {
	if !line.hasRateLimits() {
		return nil
	}
	data, err := json.Marshal(Record{RecordedAt: now.UTC(), RateLimits: line.RateLimits})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".hat-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func Load(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// namedWindows are the rate_limits windows hat shows, in order. The status
// line sends five_hour and seven_day, each {used_percentage, resets_at in unix
// seconds}, and spend_limit only through a gateway (Claude Code 2.1.330).
var namedWindows = []struct{ Key, Label string }{
	{"five_hour", "5-hour"},
	{"seven_day", "Week"},
}

func LimitsFrom(rateLimits json.RawMessage) ([]Limit, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rateLimits, &fields); err != nil {
		return nil, err
	}
	limits := []Limit{}
	for _, named := range namedWindows {
		var window struct {
			UsedPercentage *float64 `json:"used_percentage"`
			ResetsAt       *float64 `json:"resets_at"`
		}
		raw, ok := fields[named.Key]
		if !ok || json.Unmarshal(raw, &window) != nil || window.UsedPercentage == nil {
			continue
		}
		limits = append(limits, Limit{Kind: named.Key, Label: named.Label, Percent: *window.UsedPercentage, ResetsAt: fromUnixSeconds(window.ResetsAt)})
	}
	return limits, nil
}

func fromUnixSeconds(seconds *float64) *time.Time {
	if seconds == nil {
		return nil
	}
	whole, fraction := math.Modf(*seconds)
	at := time.Unix(int64(whole), int64(fraction*1e9))
	return &at
}

const HowToRecord = `No usage recorded for this account yet. Set "statusLine" in Claude Code's settings.json to the command "hat statusline" (the README shows how), then send one message as this account.`

// Recorded builds an account's row from what its status line last recorded
// in usageFile. It never reads the account's token. A window whose reset time
// has passed is left out, as Claude Code itself drops it: its old percentage
// no longer says anything.
func Recorded(acct account.Account, usageFile string, now time.Time) Row {
	doc, _ := state.Read(acct.StateFile)
	row := Row{Account: acct.Name, Kind: kindNames[acct.Kind], Email: doc.Email(), Limits: []Limit{}}

	record, err := Load(usageFile)
	if errors.Is(err, fs.ErrNotExist) {
		row.Problem = nothingRecorded(acct)
		return row
	}
	var limits []Limit
	if err == nil {
		limits, err = LimitsFrom(record.RateLimits)
	}
	if err != nil {
		row.Problem = &Problem{NoUsageRecorded, fmt.Sprintf("Couldn't read the recorded usage in %s (%v). It's rewritten the next time Claude's status line runs as this account.", usageFile, err)}
		return row
	}
	for _, limit := range limits {
		if limit.ResetsAt == nil || limit.ResetsAt.After(now) {
			row.Limits = append(row.Limits, limit)
		}
	}
	row.AsOf = &record.RecordedAt
	return row
}

func nothingRecorded(acct account.Account) *Problem {
	if _, err := os.Stat(acct.CredentialsFile); errors.Is(err, fs.ErrNotExist) {
		return &Problem{NotSignedIn, "Not signed in. Run: " + acct.SignInHint()}
	}
	return &Problem{NoUsageRecorded, HowToRecord}
}

func DefaultLine(line StatusLine) string {
	var parts []string
	if model := terminal.Printable(line.Model.DisplayName); model != "" {
		parts = append(parts, model)
	}
	if line.hasRateLimits() {
		limits, _ := LimitsFrom(line.RateLimits)
		short := map[string]string{"five_hour": "5h", "seven_day": "week"}
		for _, limit := range limits {
			if name, ok := short[limit.Kind]; ok {
				parts = append(parts, fmt.Sprintf("%s %.0f%%", name, math.Round(limit.Percent)))
			}
		}
	}
	return strings.Join(parts, " · ")
}
