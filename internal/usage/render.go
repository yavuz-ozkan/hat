package usage

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

const barWidth = 20

type Style struct {
	Bold, Reset, Green, Yellow, Red, Dim string
}

var Colors = Style{Bold: "\x1b[1m", Reset: "\x1b[0m", Green: "\x1b[32m", Yellow: "\x1b[33m", Red: "\x1b[31m", Dim: "\x1b[90m"}

func Render(w io.Writer, rows []Row, s Style, now time.Time) {
	fmt.Fprintln(w)
	for _, row := range rows {
		email := printable(row.Email)
		if email == "" {
			email = "(no account yet)"
		}
		fmt.Fprintf(w, "%s%-10s%s %s%s\n", s.Bold, printable(row.Account), s.Reset, email, ageText(row.AsOf, now, s))
		switch {
		case row.Problem != nil:
			fmt.Fprintf(w, "  %s%s%s\n", s.Yellow, printable(row.Problem.Message), s.Reset)
		case len(row.Limits) == 0:
			fmt.Fprintln(w, "  No current plan limits.")
		}
		for _, limit := range row.Limits {
			label := printable(limit.Label)
			if model := printable(limit.Model); model != "" {
				label += ", " + model
			}
			fmt.Fprintf(w, "  %-14s %s %4.0f%%  %s\n", label, bar(limit.Percent, s), math.Round(limit.Percent), resetText(limit.ResetsAt, now))
		}
		fmt.Fprintln(w)
	}
}

const LiveFooter = "Unofficial: --live calls an undocumented Anthropic endpoint that may change or stop working. Plain hat usage reads what Claude Code hands its status line."

func ageText(asOf *time.Time, now time.Time, s Style) string {
	if asOf == nil {
		return ""
	}
	age := now.Sub(*asOf)
	var ago string
	switch {
	case age < time.Minute:
		ago = "just now"
	case age < time.Hour:
		ago = fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 48*time.Hour:
		ago = fmt.Sprintf("%dh ago", int(age.Hours()))
	default:
		ago = fmt.Sprintf("%dd ago", int(age.Hours())/24)
	}
	return fmt.Sprintf("  %sas of %s%s", s.Dim, ago, s.Reset)
}

func bar(percent float64, s Style) string {
	filled := int(math.Min(barWidth, math.Max(0, math.Round(percent*barWidth/100))))
	color := s.Green
	switch {
	case percent >= 90:
		color = s.Red
	case percent >= 60:
		color = s.Yellow
	}
	return color + strings.Repeat("█", filled) + s.Reset + s.Dim + strings.Repeat("░", barWidth-filled) + s.Reset
}

func resetText(at *time.Time, now time.Time) string {
	if at == nil {
		return ""
	}
	when := at.Local().Format("Mon 3:04 PM")
	left := at.Sub(now)
	switch {
	case left < 0:
		return fmt.Sprintf("resetting now (%s)", when)
	case left >= 24*time.Hour:
		return fmt.Sprintf("resets in %dd %dh (%s)", int(left.Hours())/24, int(left.Hours())%24, when)
	default:
		return fmt.Sprintf("resets in %dh %dm (%s)", int(left.Hours()), int(left.Minutes())%60, when)
	}
}
