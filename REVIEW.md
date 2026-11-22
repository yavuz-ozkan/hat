# Review

## What matters
- hat never writes to `~/.claude`, `~/.claude.json`, or a `~/.claude-*` folder it didn't make. `account.kindOf` decides which folders it made (the `.hat` marker, the legacy `.claude-as` marker, or a `CLAUDE.md` linked to main's); anything unmanaged is read only.
- `hat usage` (the default) and `hat statusline` never read or send an account's token. They work only from what Claude Code hands its status line, recorded under the user cache folder (`layout.UsageDir`), never inside a Claude config folder.
- `hat usage --live` is the only path that reads a token. It goes only to Anthropic's usage endpoint, as a Bearer header, through a client that never follows redirects (`usage.LiveClient`). It is never printed, logged, or put in an error message.
- `hat statusline` never fails the status line: whatever goes wrong while recording, it still prints, and a wrapped command's output and exit code pass through unchanged.
- hat never renews a sign-in itself. Claude Code renews it when `claude mcp list` runs as that account (`claude.Runner.RenewSignIn`). Two copies renewing one sign-in sign each other out.
- On Windows, hat never runs Claude through a `.cmd` or `.bat` (`claude.Find`): `cmd.exe` would act on characters in the arguments hat passes.
- `rm` must never delete what an account's links point at. `share.TestRemoveAllKeepsWhatLinksPointAt` is the proof on both Windows and Linux.
- `hat upgrade` and both installers install only an archive whose SHA-256 matches the release's `checksums.txt`, and a failed install leaves the previous hat working.
- Error messages are for the person at the terminal: what went wrong and the next command to run.

## Known noise
- `memory.Key`, `memory.maxKeyLength`, the `claude mcp list` renewal, the status-line `rate_limits` shape, and the `--live` usage endpoint mirror Claude Code 2.1.330 behavior we don't control. A finding that these "should" behave differently needs evidence from Claude Code itself.
