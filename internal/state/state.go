package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

// Document is a parsed state file. Numbers stay as json.Number so they're
// written back exactly as Claude wrote them.
type Document map[string]any

const lockRetryEvery = 50 * time.Millisecond

var lockWaitFor = 5 * time.Second

var ErrBusy = errors.New("Claude was saving this account's state, so its connector list wasn't updated this time. It updates on the next start")

func Read(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Document{}, nil
	}
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	doc := Document{}
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s isn't valid JSON (%v). hat didn't change it. Fix or restore the file, then run hat again", path, err)
	}
	return doc, nil
}

// Write replaces the file in one step, so Claude never reads a half-written one.
func Write(path string, doc Document) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".hat-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(buf.Bytes()); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func (d Document) Email() string {
	account, _ := d["oauthAccount"].(map[string]any)
	email, _ := account["emailAddress"].(string)
	return email
}

func (d Document) connectors() map[string]any {
	servers, _ := d["mcpServers"].(map[string]any)
	return servers
}

func InheritFromMain(mainStateFile, accountStateFile string) error {
	main, err := Read(mainStateFile)
	if err != nil {
		return err
	}
	return withLock(accountStateFile, func() error {
		_, statErr := os.Stat(accountStateFile)
		isNew := errors.Is(statErr, fs.ErrNotExist)
		account, err := Read(accountStateFile)
		if err != nil {
			return err
		}

		merged := map[string]any{}
		for name, server := range account.connectors() {
			merged[name] = server
		}
		for name, server := range main.connectors() {
			merged[name] = server
		}
		if !isNew && reflect.DeepEqual(merged, account.connectors()) {
			return nil
		}
		if isNew {
			account["hasCompletedOnboarding"] = true
		}
		account["mcpServers"] = merged
		return Write(accountStateFile, account)
	})
}

// Claude Code guards its state file with proper-lockfile: holding the lock
// means having created the folder "<file>.lock" (Claude Code 2.1.329).
// hat takes the same lock so its writes never interleave with Claude's
// or another hat. It never removes a lock it didn't create, even an
// abandoned one: between checking a lock's age and removing it, Claude could
// replace it with a live one. Claude clears abandoned locks on its next save.
func withLock(path string, fn func() error) error {
	lock := path + ".lock"
	deadline := time.Now().Add(lockWaitFor)
	for {
		err := os.Mkdir(lock, 0o755)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) && !lockBeingReleased(err) {
			return err
		}
		if time.Now().After(deadline) {
			if _, statErr := os.Stat(lock); os.IsNotExist(statErr) {
				return err
			}
			return ErrBusy
		}
		time.Sleep(lockRetryEvery)
	}
	defer os.Remove(lock)
	return fn()
}
