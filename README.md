# release-align

release-align moves the clones listed in a workspace file onto exact revisions, and stops the run if origin cannot be reached.

The clones sit side by side under `--base-dir`. There is no superproject. Each selected project is sent to `origin/<branch>`, to a tag, or to a full commit named in the file. A dirty work tree, an unfinished rebase, a detached HEAD that is not already the pin, an upstream on some other remote, and local commits that have not been pushed are left untouched.

`mr`, `gita`, and `git submodule foreach` run one command in every copy. They have no shared stop when SSH to GitLab dies. `submodule foreach` also requires a superproject.

A loop of `git pull` waits out its own timeout in each repository and merges however that repository is configured. Nothing asks whether origin answers before the first fetch.

`go get` and `go.work` change module requirements. The service directory stays on the commit it was cloned at. The GitLab web UI updates the project on the server and never sees uncommitted files on the machine.

Module path: `github.com/oshokin/release-align`. Building needs Go 1.27.1. Running needs Git 2.31 or newer, and either the installed OpenSSH or an SSH command of your own in `GIT_SSH_COMMAND` or `GIT_SSH`. The program builds and runs on Linux, macOS, and Windows.

## Quick start

```bash
go build -o release-align .

release-align workspace init \
  --base-dir "$HOME/go/src/gitlab.stageoffice.ru" \
  --branch Release-26.3.0 \
  --file ./mailion.workspace.json

# Plan from refs already on disk. No network, no writes.
./release-align --workspace ./mailion.workspace.json --dry-run

./release-align --workspace ./mailion.workspace.json --jobs 4

./release-align --help
./release-align version
```

## Commands

The program takes no positional arguments. `--workspace` is required for a sync or a status check. An explicit flag wins over the environment variable for the same setting, and that variable is not parsed when the flag is present. `help`, `version`, `--version`, and `completion` still run when the environment is invalid, and they do not open repositories. After the flags have been parsed, a bad value or a bad workspace document exits 2 before any repository is touched. A value the parser itself rejects (`--jobs bad`, an unknown flag) is Cobra's own message: stdout is empty and the status is 2. Help and version stay text as well. When `--output json` is selected, a usage or runtime error of that run is one JSON object on stdout and progress stays on stderr. JSON is not promised before the parser can tell which command is running, and a run does not mix JSON with text.

### release-align

Prepares the clones named in the workspace file.

```bash
release-align --base-dir ~/src --workspace ./mailion.workspace.json --jobs 4 --log-level debug
release-align --workspace ./mailion.workspace.json -n -j 8 -l warn
release-align --workspace ./mailion.workspace.json --group search --repo storage/dispersed-object-store
```

| Flag | Short | Default | Environment | Meaning |
| --- | --- | --- | --- | --- |
| `--help` | `-h` | | | Print help and exit |
| `--version` | `-v` | | | Print the version line and exit |
| `--base-dir` | | `$HOME/go/src/gitlab.stageoffice.ru` | `BASE_DIR` | Root that contains the clones. `~` and `~/...` expand to the home directory. The path is stored absolute. An empty value is an error |
| `--branch` | | `master` | | Replaces `default_branch` only when this flag is present. The built-in default does not |
| `--workspace` | | | | Workspace JSON. Required |
| `--repo` | | | | Select a project by its relative path. Repeatable. Combined with `--group` as a union |
| `--group` | | | | Select a group. Repeatable |
| `--dry-run` | `-n` | `false` | `DRY_RUN` | Plan from refs already on disk. No probe, fetch, switch, or merge |
| `--log-level` | `-l` | `info` | `LOG_LEVEL` | `debug`, `info`, `warn`, or `error`, any case |
| `--jobs` | `-j` | `4` | `JOBS` | Repositories processed at once, from 1 to 64 |
| `--attempts` | | `3` | `ATTEMPTS` | Origin checks in total, including the first, from 1 to 10. `1` does not start the retry engine |
| `--probe-timeout` | | `5s` | `PROBE_TIMEOUT` | Timeout of one `git ls-remote` |
| `--retry-delay` | | `1s` | `RETRY_DELAY` | Pause between failed origin checks. `0` is allowed |
| `--fetch-timeout` | | `1m` | `FETCH_TIMEOUT` | Timeout of one fetch |
| `--local-timeout` | | `40s` | `LOCAL_TIMEOUT` | Timeout of one local Git command |
| `--output` | | `text` | | `text` or `json` |

A duration needs a Go unit on the command line and in the environment: `5s`, `750ms`, `1m`. `DRY_RUN` accepts `1`, `t`, `T`, `true`, `TRUE`, `True`, `0`, `f`, `F`, `false`, `FALSE`, and `False`.

### release-align version

Prints one line and exits 0:

```text
release-align <version> (commit <sha>, built <time>)
```

A build without those linker values prints `dev`, `unknown`, and `unknown`. `release-align version` and `release-align --version` print the same line. `-v` is this switch. It does not change the log level.

### release-align help

`release-align help`, `--help`, and `-h` print help for sync. `release-align help version` and `release-align help completion` print help for those commands. `release-align completion --help` does the same for completion.

### release-align completion

Writes a shell completion script to stdout.

```bash
release-align completion bash
release-align completion zsh
release-align completion fish
release-align completion powershell
```

`--no-descriptions` leaves flag descriptions out of the script. Bash needs the `bash-completion` package. The current bash session can load the script with `source <(release-align completion bash)`.

### release-align status

Reads cached refs and worktrees. It does not contact a remote and does not accept `--dry-run`.

```bash
release-align status --base-dir "$HOME/src/gitlab.stageoffice.ru" \
  --workspace ./mailion.workspace.json --group search --output json
```

## Workspace file

`workspace init` writes the file from clones that are already on disk. `--base-dir`, `--branch`, and `--file` are required. `--release` is an optional label. `--file` is the new JSON path, not the report format (`--output`). The base directory is not stored in the file: paths stay relative to whatever `--base-dir` you pass later. The branch name is checked for syntax only. Sync and status are what notice a missing branch, a dirty tree, or a commit that is not local yet. `init` does not read the environment variables of sync.

```bash
release-align workspace init \
  --base-dir "$HOME/src/gitlab.stageoffice.ru" \
  --branch Release-26.3.0 \
  --release 'Mailion 26.3.0' \
  --file ./mailion.workspace.json
```

A group is the relative path of a parent directory. Every ancestor prefix is recorded. A repository that sits directly under the base directory has no group. `mailion/search` and `another/search` stay separate, because the group is `mailion/search`, not the bare name `search`. `--group mailion` selects everything under that directory. `--group mailion/search` selects that subgroup. These are folder names, not a guess about which services depend on each other, and not a check of GitLab namespaces.

| Directory under the base | Groups |
| --- | --- |
| `search/pasifae` | `search` |
| `mailion/search/pasifae` | `mailion`, `mailion/search` |
| `mailion/storage/dos` | `mailion`, `mailion/storage` |
| `standalone` | none |

The scan walks the base directory, skips hidden directories, and does not follow a directory symlink. A `.git` directory or a regular gitfile marks a working tree. A `.git` symlink is rejected. The candidate must be the real repository root. Nested clones inside a found root, including vendor and submodules, are not added. A bare repository is skipped and its object database is not walked. If the base directory itself is a repository, the command stops and asks for the parent directory. A broken `.git`, a read error, or a repository without `origin` stops the command. The file is not written. An empty tree is an error too. A service that was never cloned cannot appear. The file is a starting inventory, not proof that the server has nothing else. Read it, add any missing paths by hand, and keep it.

The document is validated, including the 1 MiB limit, before the destination is opened. The file is created with mode `0600` and only if that path does not exist. A symlink at the destination is also refused. There is no `--force`. The parent directory is not created for you. If the write or close fails, the new partial file is removed. An older file is not replaced. A crash can leave a partial new file; reading it fails as bad JSON. To regenerate, pick another filename and diff:

```bash
release-align workspace init --base-dir "$HOME/src/gitlab.stageoffice.ru" \
  --branch Release-26.3.0 --file ./workspace.candidate.json
diff -u ./mailion.workspace.json ./workspace.candidate.json
```

A project revision is one of `branch`, `tag`, or `commit`. If it is omitted, the file's `default_branch` is used. `--branch` replaces that default only when the flag is present on the command line. A commit must be a full lowercase SHA-1 or SHA-256. The branch resolves to `origin/<branch>`, the tag to its peeled commit, and the commit to that object. Another branch, another tag, and the current upstream are not substitutes.

A dirty tree, an unfinished merge or rebase, an unpushed branch, or a detached HEAD that is not already the pinned commit is reported and left untouched. Local commits and uncommitted edits are not carried forward and are not discarded.

Sync checks origin (`--attempts 3` is three tries total), fetches the selected clones, resolves every target, and switches nothing if any selected project is already blocked. A successful probe is remembered for that host only. A permission failure is recorded on the repository that was checked; the next repository on the same host is still probed. If one selected repository is blocked, the others are not checked out. A repository that answered is `plan_blocked`, not a copy of the first failure. A network failure still stops the run after the shared attempt budget.

The fast-forward then uses that resolved commit. Afterward the worktree is read again. `ready` comes from that read. If switch or the fast-forward fails, the worktree is read once more and that later read is `actual`. When the read cannot be done, or the run is already canceled, `actual` is omitted. The branch from before the switch is not reported as current. There is no rollback.

The update itself is a local `git merge --ff-only` from the resolved commit. There is no second network request, no merge commit, no rebase, and no push. Git still runs hooks and filters, under the same command timeout. Recursive submodule checkout and fetch are off. `switch` and the fast-forward pass `--no-overwrite-ignore`, so a local ignored file is left in place.

Fetch may update remote-tracking refs even when the worktrees stay put; the report then has `freshness` `fetched`. `status` and `--dry-run` do not contact the network (`freshness` `cached`). Those commands set `GIT_NO_LAZY_FETCH=1` on the Git processes they start, so a partial clone does not download a missing object to answer them. A normal sync does not set that variable. The parent process environment is left unchanged. A dry-run exits 0 when the cached plan has no blocker, and `ready` is still false. URL userinfo and the credential query parameters `token`, `access_token`, `private_token`, `password`, `oauth_token`, and `secret` are removed from Git diagnostics before they are logged or printed. That does not cover an arbitrary secret from a hook or a credential helper.

`--output json` writes one JSON object to stdout. Progress and logs go to stderr. `expected_count` is the selection, `inventory_count` is the whole file, and `scope` is `workspace` or `selection`. `coverage_complete` means every selected path has a row, not that every clone exists.

| Exit | Meaning |
| --- | --- |
| 0 | Every selected project matches, or a dry-run plan has no blocker |
| 1 | Git, network, access, or I/O failure, including a JSON write error |
| 2 | Flags or the workspace document were rejected before fetch |
| 3 | The selection is not ready |
| 130 | Ctrl+C |

`status` cannot see commits that are only on the server. A workspace file may name full commit IDs directly. This build does not write a freeze file and does not add worktrees. Missing clones are not created. Nothing is pushed, tagged, or built.

## Network and parallelism

With three attempts, a 5s probe, and a 1s pause, preflight is bounded by 17s plus the time taken to stop child processes. That figure is only the preflight budget, not a bound on fetch or checkout. Fetch and checkout wait for a successful preflight.

Network errors and timeouts are retried. A rejected key, a permission failure, a missing repository, and an untrusted TLS certificate fail that one repository. The others continue. Git is started with `LC_ALL=C`, and the error text is what classifies the failure. An unrecognized error does not start an open-ended retry loop.

If the network drops after preflight, a failed fetch checks origin again. Three network failures cancel the repositories still waiting and the Git processes still running. The checks are serialized, so once the outage is confirmed a worker does not run a retry loop of its own. A successful check allows one more fetch. While a fetch is still running, the break can go unnoticed until `--fetch-timeout` expires. There is no background poll of the server.

Commands inside one repository run one after another. Worktrees that share a git directory are serialized too. `--jobs 1` processes repositories strictly in order. On Linux and macOS a timeout kills the Git process group, SSH included. On Windows the same job is done with `taskkill /T /F`, under its own time limit.

Ctrl+C exits with status 130. Repositories already switched stay switched. There is no rollback of the whole run.

Each log line is text: the local clock (`2026-10-05 14:20:15`), the level, the repository name, and the message. Paths that block an update are listed under that line, one path per line, at most 10. The rest are a single `... and N more` line. `--log-level debug` lists every path and adds fetch and probe detail. Git's own transcript stays quiet. On a terminal the level and the repository name are colored. `NO_COLOR` leaves the text uncolored. Lines from different workers are not interleaved.

A sync creates `<base-dir>/.release-align.lock` and removes it on exit. SIGKILL or a power loss can leave the directory behind. Confirm that the process is gone, then delete that directory. The program never deletes `.git/index.lock`. Other Git clients do not consult this lock, so a second pass over the same clones should not run at the same time.

## Environment

An explicit flag overrides the environment variable of the same setting. When the flag is present, that variable is not parsed, so an invalid environment value does not reject a valid flag. `workspace init` does not read this table. `--branch` has no environment variable.

| Variable | Flag |
| --- | --- |
| `BASE_DIR` | `--base-dir` |
| `DRY_RUN` | `--dry-run` |
| `LOG_LEVEL` | `--log-level`, `-l` |
| `FETCH_TIMEOUT` | `--fetch-timeout` |
| `PROBE_TIMEOUT` | `--probe-timeout` |
| `LOCAL_TIMEOUT` | `--local-timeout` |
| `JOBS` | `--jobs` |
| `ATTEMPTS` | `--attempts` |
| `RETRY_DELAY` | `--retry-delay` |

Durations need a unit in the environment as well: `5s`, `750ms`, `1m`. The default level is `info`. `LOG_LEVEL` accepts the same names as `--log-level`, in any case.

Git is not asked for a password. Existing credentials and `ssh-agent` are used. If `GIT_SSH_COMMAND` or `GIT_SSH` is set, that value is kept, and the outer timeout still applies. Otherwise OpenSSH is started with `BatchMode=yes`, `ConnectTimeout=5`, one connection attempt, and `StrictHostKeyChecking=accept-new`. A new host key may be written to `known_hosts` on a normal run. Add a passphrase-protected key with `ssh-add` beforehand.

## Building

The code lives in `cmd` and in four packages under `internal`: `app` (the workspace inventory and the sync), `gitter` (running Git and applying process timeouts), `retry` (repeating the origin check), and `logger` (zap, and writes from several workers).

`--attempts 3` allows two retries after the first check. `--attempts 1` makes one call and does not start the retry engine. Inside the engine, zero retries means no limit, so a single attempt deliberately skips the engine. Canceling the parent context stops retries at once. A timeout of one Git command is retried while that parent context is still alive.

```bash
./release-align --workspace ./mailion.workspace.json -n -j 8 -l debug
./release-align -v
./release-align completion bash > release-align-completion.bash
```

Go 1.27.1 is pinned in `go.mod`, and CI reads the version from there. golangci-lint v2.14.0 is pinned in the Taskfile and in the workflow. Formatting is gofumpt, gci, and golines.

```bash
task lint
task test-race
task build

# The same steps without Task.
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
golangci-lint config verify
golangci-lint run
go test -race -shuffle=on ./...
go build -trimpath -o release-align .
```

`task fmt` runs the formatters. `task check` runs lint and the race tests. `task install-lint` installs the pinned golangci-lint into `./bin`. CI fails if formatting would change the tree. Build and test run on Linux, macOS, and Windows. The race detector runs on Linux and macOS.

Tests create local bare repositories. They do not need GitLab. A dropped SSH connection is simulated by a local helper. Tests that depend on Unix process groups are skipped on Windows.

`.goreleaser.yaml` builds the release archives. `cmd.Version`, `cmd.Commit`, and `cmd.BuildTime` are linked into the executable. After a successful `Build and Test` workflow on `master` or `main`, the `Release` workflow computes the next tag with `scripts/semver_next.sh`: `feat` bumps minor, `major` bumps major, and anything else bumps patch. GoReleaser publishes the result. `task version-check` prints the next tag locally. `task install-githooks` installs the `.githooks/commit-msg` hook.

The MIT license is in `LICENSE`. Copyright (c) 2026 Oleg Shokin.
