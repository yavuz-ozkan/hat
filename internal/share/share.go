package share

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var SharedAllowlist = []string{
	"CLAUDE.md",
	"settings.json",
	"settings.local.json",
	"history.jsonl",
	"skills",
	"agents",
	"commands",
	"output-styles",
	"plugins",
	"projects",
	"file-history",
}

var perAccount = map[string]string{
	".credentials.json": "its sign-in",
	".claude.json":      "its state and connector sign-ins",
	".hat":              "the hat marker",
	".claude-as":        "the hat marker from before hat was renamed",
	"browser-profile":   "its saved website logins",
}

func Items(alsoShare []string) ([]string, error) {
	seen := map[string]bool{}
	var items []string
	for _, name := range SharedAllowlist {
		seen[name] = true
		items = append(items, name)
	}
	for _, name := range alsoShare {
		if err := checkShareable(name); err != nil {
			return nil, err
		}
		if !seen[name] {
			seen[name] = true
			items = append(items, name)
		}
	}
	return items, nil
}

func checkShareable(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || filepath.IsAbs(name) {
		return fmt.Errorf("alsoShare entries are names of files or folders directly inside the main config folder, like \"my-skill-data\"; %q isn't one. Fix it in alsoShare", name)
	}
	// Windows drops a trailing dot or space from a name and reads "a:b" as the
	// stream b of file a, so such a name reaches a different file than it
	// spells, possibly one the check below would refuse.
	if runtime.GOOS == "windows" && (strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || strings.Contains(name, ":")) {
		return fmt.Errorf("alsoShare lists %q, but Windows reads a name ending in a dot or space, or holding a colon, as a different file. Write the name exactly as the file or folder is named, like \"my-skill-data\"", name)
	}
	for owned, holds := range perAccount {
		if filepath.Clean(name) == owned || (runtime.GOOS == "windows" && strings.EqualFold(name, owned)) {
			return fmt.Errorf("alsoShare lists %q, which holds %s for a single account, so sharing it would make every account use the same one. Remove it from alsoShare", name, holds)
		}
	}
	return nil
}

// PrivateCopy reports a shared item that exists in the account folder as a
// real file or folder instead of a link. Claude does this when it replaces a
// link while saving; whatever changed under that account sits in the copy.
type PrivateCopy struct {
	Item   string
	Copy   string
	Shared string
}

func (p PrivateCopy) Error() string {
	return fmt.Sprintf("'%s' under this account is a private copy, not shared with the other accounts. "+
		"Compare %s with %s, keep what you want in %s, delete %s, then run hat again to re-link it.",
		p.Item, p.Copy, p.Shared, p.Shared, p.Copy)
}

// OnlyHere reports an item the account holds for real while the main folder
// has none. Claude creates it the first time it needs it, so the other
// accounts can't see it until it lives in the main folder.
type OnlyHere struct {
	Item   string
	Path   string
	Shared string
}

func (o OnlyHere) Error() string {
	return fmt.Sprintf("'%s' exists only under this account, so the other accounts can't see it. "+
		"Move %s to %s, then run hat again to share it.", o.Item, o.Path, o.Shared)
}

type Warnings []error

func Connect(accountDir, mainDir string, items []string) (Warnings, error) {
	var warnings Warnings
	for _, item := range items {
		target := filepath.Join(mainDir, item)
		link := filepath.Join(accountDir, item)
		targetInfo, err := os.Stat(target)
		if os.IsNotExist(err) {
			if isLink, _ := isLink(link); !isLink {
				if _, err := os.Lstat(link); err == nil {
					warnings = append(warnings, OnlyHere{Item: item, Path: link, Shared: target})
				}
			}
			continue
		}
		if err != nil {
			return warnings, err
		}
		linked, exists, err := pointsTo(link, target)
		if err != nil {
			return warnings, err
		}
		if linked {
			continue
		}
		if exists {
			warnings = append(warnings, PrivateCopy{Item: item, Copy: link, Shared: target})
			continue
		}
		if err := makeLink(link, target, targetInfo.IsDir()); err != nil {
			return warnings, err
		}
	}
	return warnings, nil
}

func IsLinkTo(path, target string) bool {
	linked, _, err := pointsTo(path, target)
	return err == nil && linked
}

func pointsTo(link, target string) (linked, exists bool, err error) {
	if _, err := os.Lstat(link); os.IsNotExist(err) {
		return false, false, nil
	} else if err != nil {
		return false, false, err
	}
	isLink, err := isLink(link)
	if err != nil || !isLink {
		return false, true, err
	}
	dest, err := readLink(link)
	if err != nil {
		return false, true, err
	}
	return samePath(dest, target), true, nil
}
