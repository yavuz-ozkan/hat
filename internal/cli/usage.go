package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/yavuz-ozkan/hat/internal/account"
	"github.com/yavuz-ozkan/hat/internal/claude"
	"github.com/yavuz-ozkan/hat/internal/terminal"
	"github.com/yavuz-ozkan/hat/internal/usage"
)

func (a App) usage(args []string) int {
	asJSON, live := false, false
	for _, arg := range args {
		switch arg {
		case "--json":
			asJSON = true
		case "--live":
			live = true
		default:
			return a.fail(exitBadArgs, "usage doesn't take %q. Run hat usage, add --json for machine-readable output, or --live to ask Anthropic right now.", arg)
		}
	}
	accounts, err := account.All(a.Layout)
	if err != nil {
		return a.fail(exitFailed, "couldn't list accounts in %s: %v", a.Layout.Home, err)
	}
	var rows []usage.Row
	if live {
		rows = a.liveRows(accounts)
	} else {
		rows = a.recordedRows(accounts)
	}

	if asJSON {
		encoder := json.NewEncoder(a.Out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(rows); err != nil {
			return a.fail(exitFailed, "%v", err)
		}
		return exitOK
	}
	style := usage.Style{}
	if terminal.ColorOK() {
		style = usage.Colors
	}
	usage.Render(a.Out, rows, style, time.Now())
	if live {
		fmt.Fprintf(a.Out, "%s%s%s\n", style.Dim, usage.LiveFooter, style.Reset)
	}
	return exitOK
}

func (a App) recordedRows(accounts []account.Account) []usage.Row {
	now := time.Now()
	rows := make([]usage.Row, 0, len(accounts))
	for _, acct := range accounts {
		path, _ := a.Layout.UsageFile(acct.ConfigDir)
		rows = append(rows, usage.Recorded(acct, path, now))
	}
	return rows
}

func (a App) liveRows(accounts []account.Account) []usage.Row {
	checker := usage.Checker{Client: usage.LiveClient(), Endpoint: usage.Endpoint, Now: time.Now}
	rows := make([]usage.Row, 0, len(accounts))
	for _, acct := range accounts {
		if usage.NeedsRenewal(acct, time.Now()) {
			a.renew(acct)
		}
		rows = append(rows, checker.Measure(acct))
	}
	return rows
}

func (a App) renew(acct account.Account) {
	runner, err := a.runner()
	if err != nil {
		say(a.Err, "hat: warning: couldn't renew %s's expired sign-in: %v", acct.Name, err)
		return
	}
	say(a.Err, "Renewing %s's expired sign-in through Claude...", acct.Name)
	set, unset := acct.Environment()
	runner.RenewSignIn(claude.Env(set, unset...), acct.ConfigDir)
}
