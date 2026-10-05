# release-align

release-align moves a directory of local Git clones onto one release, and stops the whole walk if origin cannot be reached.

The clones sit side by side, in the GOPATH layout `<base-dir>/<group>/<repo>`. There is no superproject. Each service is sent to `origin/<branch>`, to the origin branch that contains the commit listed in the version table, to the tag from that table, or to the current branch when that branch already tracks origin. A dirty work tree, an unfinished rebase, a detached HEAD, an upstream on some other remote, and local commits that have not been pushed are left untouched.

`mr`, `gita`, and `git submodule foreach` run one command in every copy. They have no rule for picking a branch from a version table, and they have no shared stop when SSH to GitLab dies. `submodule foreach` also requires a superproject.

A loop of `git pull` waits out its own timeout in each repository and merges however that repository is configured. Nothing asks whether origin answers before the first fetch.

`go get` and `go.work` change module requirements. The service directory stays on the commit it was cloned at. The GitLab web UI updates the project on the server and never sees uncommitted files on the machine.

Module path: `github.com/oshokin/release-align`. Building needs Go 1.27.1. Running needs Git 2.31 or newer, and either the installed OpenSSH or an SSH command of your own in `GIT_SSH_COMMAND` or `GIT_SSH`. The program builds and runs on Linux, macOS, and Windows.

## Quick start

```bash
go build -o release-align .

# Plan from refs already on disk. No network, no writes.
./release-align --dry-run

# Default branch: master.
./release-align

# One release branch for the tree.
./release-align --branch Release-2.3.2 --jobs 4

# Another directory and another version table.
./release-align --base-dir "$HOME/go/src/gitlab.stageoffice.ru" \
  --branch Release-2.3.2 --versions-file "$HOME/versions.json"

./release-align --help
./release-align version
```

By default the walk takes repositories at depth 2 only: `<base-dir>/<group>/<repo>`. `--depth 3` takes level 3 only. Hidden directories are skipped. Directory symlinks are not followed. Both a `.git` directory and a worktree `.git` file qualify.

## What happens to a repository

1. A local check: the work tree is clean, no merge, rebase, cherry-pick, revert, or bisect is in progress, HEAD is on a branch, that branch has an upstream on origin, and the branch has no commits that origin does not already have.
2. Before the first fetch, and once per transport and host, the program runs `git ls-remote origin HEAD`. VPN, SSH config, credentials, and `url.*.insteadOf` all apply. There is no separate HTTP call to the GitLab web UI.
3. `git fetch --atomic --prune --tags --no-prune-tags` updates origin branches and tags. `--prune` applies to remote-tracking branches. Local tags are neither deleted nor overwritten. A tag conflict fails that repository, and the branch has not been switched yet.
4. The local check runs again against the updated refs.
5. The target is chosen in this order:

| Order | Target |
| --- | --- |
| 1 | `origin/<branch>`, `origin/master` by default |
| 2 | The first origin branch, by name, that contains the commit from the table. `origin/HEAD` and other symbolic refs are ignored |
| 3 | The tag from the table, when no branch contains that commit. HEAD is then detached |
| 4 | The current local branch and its origin upstream |

A branch that contains the listed commit may have moved past it. The program checks that branch out and fast-forwards it to current origin. The SHA in the table is used to find the branch. The tag from the table is used when no such branch exists. On the fallback path, a renamed local branch keeps its name.

If the target local branch already exists, its history must be an ancestor of the chosen origin branch, and its upstream must be that same ref. Otherwise the repository is skipped before `switch`. A missing local branch is created with tracking set to origin.

The update itself is a local `git merge --ff-only` from the chosen origin ref. There is no second network request, no merge commit, no rebase, no stash, no reset, and no push. Git still runs hooks and filters, under the same command timeout. Recursive submodule checkout and fetch are off. `switch` and the fast-forward pass `--no-overwrite-ignore`, so a local ignored file is also left in place.

## Network and parallelism

| Flag | Default | Meaning |
| --- | ---: | --- |
| `--jobs` | 4 | Repositories processed at once |
| `--attempts` | 3 | Origin checks in total, including the first |
| `--probe-timeout` | 5s | Timeout of one `ls-remote` |
| `--retry-delay` | 1s | Pause between checks |
| `--fetch-timeout` | 1m | Timeout of one fetch |
| `--local-timeout` | 40s | Timeout of one local Git command |

With three attempts, a 5s probe, and a 1s pause, preflight is bounded by 17s plus the time taken to stop child processes. Directory discovery and the local checks sit outside those 17s. Fetch and checkout wait for a successful preflight. If no repository qualifies, the program makes no network request.

Network errors and timeouts are retried. A rejected key, a permission failure, a missing repository, and an untrusted TLS certificate fail that one repository. The others continue. Git is started with `LC_ALL=C`, and the error text is what classifies the failure. An unrecognized error does not start an open-ended retry loop.

If the network drops after preflight, a failed fetch checks origin again. Three network failures cancel the repositories still waiting and the Git processes still running. The checks are serialized, so once the outage is confirmed a worker does not run a retry loop of its own. A successful check allows one more fetch. While a fetch is still running, the break can go unnoticed until `--fetch-timeout` expires. There is no background poll of the server.

Commands inside one repository run one after another. Worktrees that share a git directory are serialized too. `--jobs 1` processes repositories strictly in order. On Linux and macOS a timeout kills the Git process group, SSH included. On Windows the same job is done with `taskkill /T /F`, under its own time limit.

Ctrl+C exits with status 130. Repositories already switched stay switched. There is no rollback of the whole run. If a local command fails after `switch`, that repository is reported as `failed`.

## Version table

The executable contains 43 entries compiled from `internal/app/defaults.json`. The snapshot is fixed and is not refreshed by itself. `--versions-file` and `VERSIONS_FILE` replace entries with the same key.

JSON:

```json
{
  "spanner": "v1.23.3:8284453",
  "google/borg": "v2.44.1:126c4be",
  "chubby": ""
}
```

The key `group/repo` wins over the key `repo`. An empty string disables the built-in entry with the same key. `{}` leaves the built-in table in place. A commit is 4 to 64 hexadecimal characters. For real repositories, use at least 7 characters, or the full SHA. The file must use a `.json` extension. A broken file and a file larger than 16 MiB stop the run before any network call.

## Dry run and local work

`--dry-run` skips preflight, fetch, switch, checkout, and merge. The plan is built from refs already stored locally. Branches that exist only on the server are invisible, so a real run may pick another target or skip the repository.

`--local` chooses what happens to local commits and edits. The default is `skip`. A detached HEAD and an unfinished merge or rebase are always skipped. After a detach at a tag, the next run sees that detached HEAD again. You choose a tracking branch yourself.

`skip` leaves a dirty tree, a branch with no upstream, a branch that tracks a remote other than origin, and a branch with local commits untouched.

`keep` carries staged, unstaged, and untracked files onto the updated branch. They are stashed with `--include-untracked`, the branch is fast-forwarded or the tag is detached, and the stash is applied without restoring the index, so the edits come back unstaged. The stash entry is dropped only after that apply succeeds. A conflict leaves the entry in place and the repository is failed. If the switch or fast-forward fails, the stash is applied with `--index` so the original staged state returns, and the entry is dropped only when that restore succeeds. Ignored files stay where they are. Local commits are still skipped: putting them on top would be a rebase. `--dry-run` does not stash.

`reset` makes the selected branch match the origin ref already fetched. Local commits on that branch are discarded and remain in the reflog. Staged edits, unstaged edits, and untracked files are removed. Ignored files stay. The branch upstream is set to that origin ref. `--dry-run` reports the discarded commits and edits and does not move the branch.

Each log line is text: the local clock (`2026-10-05 14:20:15`), the level, the repository name, and the message. Paths that block an update are listed under that line, one path per line, at most 10. The rest are a single `... and N more` line. `--log-level debug` lists every path and adds fetch and probe detail. Git's own transcript stays quiet. On a terminal the level and the repository name are colored. `NO_COLOR` leaves the text uncolored. Lines from different workers are not interleaved. Repositories still queued when the run stops are counted as `canceled` and are not listed. The last line is the summary: `total`, `updated`, `planned`, `skipped`, `failed`, and `canceled`. A branch that was already current still counts as `updated` after a successful `--ff-only`.

A normal run creates `<base-dir>/.release-align.lock` and removes it on exit. SIGKILL or a power loss can leave the directory behind. Confirm that the process is gone, then delete that directory. The program never deletes `.git/index.lock`. Other Git clients do not consult this lock, so a second pass over the same clones should not run at the same time.

## Environment

A flag overrides the environment variable of the same setting. A bad value in the environment is still an error when the same setting is also passed as a flag.

| Variable | Flag |
| --- | --- |
| `BASE_DIR` | `--base-dir` |
| `RELEASE_BRANCH` | `--branch` |
| `MAX_DEPTH` | `--depth` |
| `VERSIONS_FILE` | `--versions-file` |
| `DRY_RUN` | `--dry-run` |
| `LOCAL` | `--local` |
| `LOG_LEVEL` | `--log-level`, `-l` |
| `FETCH_TIMEOUT` | `--fetch-timeout` |
| `LSREMOTE_TIMEOUT` | `--probe-timeout` |
| `CHECKOUT_TIMEOUT` | `--local-timeout` |
| `JOBS` | `--jobs` |
| `ATTEMPTS` | `--attempts` |
| `RETRY_DELAY` | `--retry-delay` |

In the environment a bare number means seconds, so `60` and `60s` are the same. Flags require a unit: `5s`, `750ms`. The default level is `info`. `LOG_LEVEL` accepts the same names as `--log-level`, in any case.

Git is not asked for a password. Existing credentials and `ssh-agent` are used. If `GIT_SSH_COMMAND` or `GIT_SSH` is set, that value is kept, and the outer timeout still applies. Otherwise OpenSSH is started with `BatchMode=yes`, `ConnectTimeout=5`, one connection attempt, and `StrictHostKeyChecking=accept-new`. A new host key may be written to `known_hosts` on a normal run. Add a passphrase-protected key with `ssh-add` beforehand.

| Status | When |
| --- | --- |
| 0 | The walk finished. Skipped repositories that were unsafe to touch do not change this |
| 1 | At least one repository failed: Git, the lock, the directory walk, or an unreachable origin |
| 2 | Bad flags, bad environment, or a bad version table |
| 130 | Ctrl+C, or another SIGINT |

## Building

The code lives in `cmd` and in four packages under `internal`: `app` (the walk, the version table, and target selection), `gitter` (running Git and applying process timeouts), `retry` (repeating the origin check), and `logger` (zap, and writes from several workers).

`--attempts 3` allows two retries after the first check. `--attempts 1` makes one call and does not start the retry engine. Inside the engine, zero retries means no limit, so a single attempt deliberately skips the engine. Canceling the parent context stops retries at once. A timeout of one Git command is retried while that parent context is still alive.

Help, `version`, `--version`, and `completion` still work when the environment is invalid, and they do not open repositories. Short flags are `-n`, `-j`, and `-v`.

```bash
./release-align -n -j 8
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

The GPL-3.0 license is in `LICENSE`. Third-party pieces are listed in `NOTICE`.
