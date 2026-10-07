# release-align

release-align prepares the clones under one directory for one release. There is no superproject. `workspace init` finds the repositories already on disk, sync fast-forwards each selected worktree onto its workspace revision, `--remote` and `workspace clone` compare that set with GitLab, and `workspace archive` packs the selected trees into one ZIP.

[West](https://docs.zephyrproject.org/latest/develop/west/manifest.html) already covers the main model: a manifest, shared and per-project revisions, groups, update, and recorded commits. [gita](https://github.com/nosarthur/gita), [repo](https://gerrit.googlesource.com/git-repo/+/HEAD/docs/manifest-format.md), and [mr](https://myrepos.branchable.com/) cover other multi-repo jobs. West is not a weaker tool for managing a set of repositories. These commands are only already fitted to this checkout:

| Task | release-align |
| --- | --- |
| Start from a directory of existing clones | `workspace init` discovers repositories and names groups from parent directories. West starts from a manifest; `west init -l` uses one that already exists. |
| Notice projects GitLab has and the disk does not | `--remote` and `workspace clone` list the saved groups, show what is missing, and clone it. |
| Stop when the shared server does not answer | One probe budget is shared. After it is spent, the run stops instead of retrying inside every repository. |
| Select one directory group | `--group mailion/search` is the selection. Enabling one west group does not disable the others. |
| Stay on the named release branch | Sync checks out that branch and fast-forwards. `west update` leaves `HEAD` detached at the commit. |
| Hand the selected sources over as one file | `workspace archive` writes one ZIP with the directory layout and a manifest of commits. |

A detached `HEAD` does not change the files in a commit, so it matters when you will commit again, not when you only read the tree. West's `forall` and extensions can add the other steps; keeping them here costs maintenance. That practical gap has not been measured on a real Mailion checkout.

Module path: `github.com/oshokin/release-align`. Building needs Go 1.27.1. Offline commands set `GIT_NO_LAZY_FETCH=1` on the Git processes they start. Suppression is best effort: a Git build that ignores the variable can still download a missing partial-clone object. That was checked with Git 2.51.1, which honors the variable; the commands also run on Git 2.43.0. A normal sync does not set the variable. Running needs OpenSSH, or your own command in `GIT_SSH_COMMAND` or `GIT_SSH`.

## Quick start

```bash
go build -o release-align .

./release-align workspace init \
  --base-dir "$HOME/go/src/gitlab.stageoffice.ru" \
  --branch Release-26.3.0 \
  --file ./mailion.workspace.json

# Plan from refs already on disk. No fetch and no writes. Lazy fetch suppression is best effort.
./release-align --workspace ./mailion.workspace.json --dry-run

./release-align --workspace ./mailion.workspace.json --jobs 4

./release-align --help
./release-align version
```

## Commands

The program takes no positional arguments. `--workspace` is required for a sync or a status check. Flag priority is below the flag table. `help`, `version`, `--version`, and `completion` still run when the environment is invalid, and they do not open repositories. After the flags have been parsed, a bad value or a bad workspace document exits 2 before any repository is touched. A value the parser itself rejects (`--jobs bad`, an unknown flag) is Cobra's own message: stdout is empty and the status is 2. Help and version stay text. When `--output json` is selected, a usage or runtime error of that run is one JSON object on stdout and progress stays on stderr. JSON is not promised before the parser can tell which command is running.

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
| `--fetch-timeout` | | `1m` | `FETCH_TIMEOUT` | Timeout of one fetch. Also the budget for one GitLab catalog read |
| `--local-timeout` | | `40s` | `LOCAL_TIMEOUT` | Timeout of one local Git command |
| `--output` | | `text` | | `text` or `json` |
| `--remote` | | `false` | | After the local operation, compare `gitlab.groups` with the disk and the workspace. Not combined with `--dry-run` |

A duration needs a Go unit: `5s`, `750ms`, `1m`. `DRY_RUN` accepts `1`, `t`, `T`, `true`, `TRUE`, `True`, `0`, `f`, `F`, `false`, `FALSE`, and `False`.

An explicit flag wins over the environment variable, which wins over `timeouts` in the workspace, which wins over the built-in default. When the flag is present, that variable is not parsed. A command that does not define the flag ignores that variable, so `workspace init` reads none of this table and `workspace refresh` reads only `timeouts.local` from the file. `workspace clone` reads `CLONE_TIMEOUT` (`--clone-timeout`, default `15m`). `workspace archive` reads `ARCHIVE_TIMEOUT` (`--archive-timeout`, default `15m` for one repository). `--branch` has no environment variable. `--retry-delay 0` is allowed. A timeout of `0` is not. Suggested commands are quoted for a POSIX shell, or for PowerShell when the program is running on Windows.

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

Reads cached refs and worktrees. Without `--remote` it does not call GitLab. It does not accept `--dry-run`. `--remote` adds the group inventory after the local check and does not clone. `freshness=cached` means the refs used for readiness were already local; it is not a certificate that every installed Git build stayed off the network.

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

`ready` describes the selected entries in the file, not every clone under the base directory. A new local clone stays invisible to `status` and sync until it is listed. A listed path that is gone is reported when that path is part of the selection.

The document is validated, including the 1 MiB limit, before the destination is opened. The file is created with mode `0600` and only if that path does not exist. A symlink at the destination is also refused. There is no `--force`. The parent directory is not created for you. If the write or close fails, the new partial file is removed. An older file is not replaced. A crash can leave a partial new file; reading it fails as bad JSON. To regenerate, pick another filename and diff:

```bash
release-align workspace init --base-dir "$HOME/src/gitlab.stageoffice.ru" \
  --branch Release-26.3.0 --file ./workspace.candidate.json
diff -u ./mailion.workspace.json ./workspace.candidate.json
```

### release-align workspace refresh

`refresh` compares that file with the clones currently under `--base-dir`. `--base-dir` and `--file` are required. With no `--add` and no `--add-all`, the command only prints the difference and leaves the file, its modification time, and every Git worktree unchanged.

```bash
./release-align workspace refresh \
  --base-dir "$HOME/go/src/gitlab.stageoffice.ru" \
  --file ./mailion.workspace.json

./release-align workspace refresh \
  --base-dir "$HOME/go/src/gitlab.stageoffice.ru" \
  --file ./mailion.workspace.json \
  --add mailion/search/new-indexer

./release-align workspace refresh \
  --base-dir "$HOME/go/src/gitlab.stageoffice.ru" \
  --file ./mailion.workspace.json \
  --add-all
```

`--add` is repeatable and is one exact relative path, not a glob. A comma stays inside that path. `--add` and `--add-all` cannot be combined. Adding a path that is already listed does nothing. Adding a path that is not a new local clone fails the whole request before the file is replaced. `--add-all` appends every new local clone and leaves listed paths that are no longer on disk in the file. A second `--add-all` with nothing new does not open the file for writing.

New entries go after the existing ones, in lexical order, with directory groups from the same rules as `init` and with no revision of their own. They inherit `default_branch`. A file whose projects are all pinned, and which has no `default_branch`, can be previewed, but a new unpinned project is rejected until you set `default_branch` or add the project by hand with a revision. Refresh does not invent `master` or `main`. Existing `release`, `default_branch`, groups, pins, and project order stay. The file may be reformatted when it actually changes.

An unlisted clone is not an error. Another project under the same directory may have been left out on purpose. A missing listed path is a warning in the preview; other valid additions are still allowed, and the missing entry is not deleted. A listed path that exists but is not a usable repository, including a broken `.git`, a missing origin, a symlink, or a permissions error, stops the command and does not write. A hidden or nested root that you put in the file by hand is checked directly, so the directory walk skipping it does not make it look deleted.

Refresh does not fetch, switch, or ask the server whether `default_branch` exists. Exit 0 means the comparison finished. Unlisted and missing paths can still be present. Exit 1 is a scan, Git, lock, or write failure. Exit 2 is flags or a workspace document that cannot be updated. The output does not say that the repositories are ready.

A repository that was created on the server but never cloned is not in this scan. `status --remote` and `workspace clone` read that list from the GitLab groups saved in the file. `refresh` does not call the API.

While it writes, refresh creates `<file>.lock` next to the workspace with `O_EXCL` and does not wait. The lock coordinates release-align processes. An editor does not take it. If a crash leaves the lock, confirm that no refresh is running, then delete that file. The program does not delete a lock it did not create and does not treat an old lock as free. A preview does not create the lock.

The replacement is a temporary file in the same directory, renamed onto the workspace name. On Unix that rename replaces the directory entry. Go does not promise the same atomicity on other operating systems, and this is not a crash-proof replace on every OS. If the rename fails, the original file stays and there is no second, destructive replace. Before the rename, refresh checks that the path is still that regular file and that its bytes are still the bytes it read. The check can notice an outside edit. It is not an atomic compare-and-swap against an editor, and two hard links to the file are not a supported way to edit it.

A project revision is one of `branch`, `tag`, or `commit`. If it is omitted, the file's `default_branch` is used. `--branch` replaces that default only when the flag is present on the command line. A commit must be a full lowercase SHA-1 or SHA-256. The branch resolves to `origin/<branch>`, the tag to its peeled commit, and the commit to that object. Another branch, another tag, and the current upstream are not substitutes.

A dirty tree, an unfinished merge or rebase, an unpushed branch, or a detached HEAD that is not already the pinned commit is reported and left untouched. Local commits and uncommitted edits are not carried forward and are not discarded.

Sync checks origin (`--attempts 3` is three tries total), fetches the selected clones, resolves every target, and switches nothing if any selected project is already blocked. A successful probe is remembered for that host only. A permission failure is recorded on the repository that was checked; the next repository on the same host is still probed. If one selected repository is blocked, the others are not checked out. A repository that answered is `plan_blocked`, not a copy of the first failure. A network failure still stops the run after the shared attempt budget.

The fast-forward then uses that resolved commit. Afterward the worktree is read again. `ready` comes from that read. If switch or the fast-forward fails, the worktree is read once more and that later read is `actual`. When the read cannot be done, or the run is already canceled, `actual` is omitted. The branch from before the switch is not reported as current. There is no rollback.

The update itself is a local `git merge --ff-only` from the resolved commit. There is no second network request, no merge commit, no rebase, and no push. Git still runs hooks and filters, under the same command timeout. Recursive submodule checkout and fetch are off. `switch` and the fast-forward pass `--no-overwrite-ignore`, so a local ignored file is left in place.

Fetch may update remote-tracking refs even when the worktrees stay put; the report then has `freshness` `fetched`. `status` and `--dry-run` do not fetch (`freshness` `cached`) and set `GIT_NO_LAZY_FETCH=1`, as described above. A dry-run exits 0 when the cached plan has no blocker, and `ready` is still false. `--dry-run --remote` is rejected; use `status --remote` to ask GitLab. URL userinfo and the credential query parameters `token`, `access_token`, `private_token`, `password`, `oauth_token`, and `secret` are removed from Git diagnostics before they are logged or printed. That does not cover an arbitrary secret from a hook or a credential helper.

`--output json` writes one JSON object to stdout. Progress and logs go to stderr. `expected_count` is the selection, `inventory_count` is the whole file, and `scope` is `workspace` or `selection`. `coverage_complete` means every selected path has a row, not that every clone exists.

| Exit | Meaning |
| --- | --- |
| 0 | Every selected project matches, or a dry-run plan has no blocker |
| 1 | Git, network, access, or I/O failure, including a JSON write error. Also a requested GitLab inventory that could not be read, even when the selected branches were already switched |
| 2 | Flags or the workspace document were rejected before fetch |
| 3 | The selection is not ready |
| 130 | Ctrl+C |

`status` cannot see commits that are only on the server. A workspace file may name full commit IDs directly. This build does not write a freeze file and does not add worktrees. Missing clones are not created by `status` or `sync`. Nothing is pushed, tagged, or built.

An optional `gitlab` object names the server scope. `gitlab.groups` are GitLab namespace paths, including subgroups. `projects[].groups` stay the local selection labels. One is not derived from the other. `clone_protocol` is `ssh` or `https`; an empty value means `ssh`. The API token is `GITLAB_TOKEN` in the environment, sent as `Private-Token`. It is not stored in the file, the command line, or a clone URL. A file with `gitlab` or `timeouts` will not load in an older binary. A file without those objects still loads. `timeouts` stores only the overrides you set (`probe`, `fetch`, `local`, `clone`, `archive`). Omitted fields keep the program default. An empty string, `0`, a negative duration, or a bare number is rejected. `workspace init --gitlab-url` and repeatable `--gitlab-group` only save that object. They do not call the API.

```bash
release-align status --workspace ./mailion.workspace.json --base-dir "$BASE_DIR" --remote

release-align workspace clone \
  --workspace ./mailion.workspace.json \
  --base-dir "$BASE_DIR" \
  --repo mailion/search/new-indexer

release-align workspace clone \
  --workspace ./mailion.workspace.json \
  --base-dir "$BASE_DIR" \
  --all
```

`workspace archive` writes one ZIP of the selected trees. `--file` is resolved from the current directory. Paths inside the archive follow `projects[].path`, for example `mailion/search/pasifae/go.mod`, plus `_release-align/manifest.json` with the resolved commit for each repository. Each project path must be the worktree root; a subdirectory of another clone is rejected. Omit `--repo` and `--group` to pack every repository. The revision is the pin, or `default_branch` when there is no pin. Those names are resolved to commits from local refs before the first `git archive`. A local branch or the current HEAD is not substituted. There is no fetch, clone, or checkout. A missing clone or object stops the command, and the destination file is not created or replaced. `git archive` exports the committed tree, so uncommitted and untracked files stay out. Tracked files stay unless `.gitattributes` marks them `export-ignore`. Submodule contents are not downloaded; gitlinks are listed in the manifest. The archive is not a byte-identical promise and it is not a full build backup.

`--remote` on `status` or on a sync checks the saved groups after the local operation and prints the difference. It does not clone and it does not change the workspace file. Without `--remote` the report says the inventory was not checked. `ready` counts selected workspace rows. Uncloned projects in the GitLab scope are a separate list, not a failed selection. Archived projects and projects shared in from outside the group are omitted. If the API does not return a complete list, the report says the catalog is unknown and does not claim that nothing is new. After a confirmed network failure or a cancel of the sync, the API is not asked again. If the branches were switched and the inventory request then fails, the message says both: alignment completed, inventory failed, nothing was cloned. The exit status is 1.

`workspace clone` downloads missing checkouts into `<base-dir>/<path_with_namespace>`, reuses a matching checkout, and appends new rows without changing pins or order. `--repo` and `--all` cannot be combined. `--all` is the saved server scope, not the whole GitLab instance and not `--group`. A project with no default branch is skipped by `--all` and rejected by `--repo`. An occupied path or a different origin is left alone. Clones run one after another, on the remote default branch, without a depth filter. A later `release-align` aligns the release. If one clone fails, finished checkouts stay on disk and completed new rows are still saved. If the JSON write fails, those checkouts stay and the output includes a recovery command. Ctrl-C does not start that write. Clone takes the base-directory lock before the workspace file lock described above.

## Network and parallelism

With three attempts, a 5s probe, and a 1s pause, preflight is bounded by 17s plus the time taken to stop child processes. That figure is only the preflight budget, not a bound on fetch or checkout. Fetch and checkout wait for a successful preflight.

Network errors and timeouts are retried. A rejected key, a permission failure, a missing repository, and an untrusted TLS certificate fail that one repository. Probe, fetch, and planning still run for the other selected repositories. Checkout of the rest does not start when any selected repository is already blocked. Git is started with `LC_ALL=C`, and the error text is what classifies the failure. An unrecognized error does not start an open-ended retry loop.

If the network drops after preflight, a failed fetch checks origin again. Three network failures cancel the repositories still waiting and the Git processes still running. The checks are serialized, so once the outage is confirmed a worker does not run a retry loop of its own. A successful check allows one more fetch. While a fetch is still running, the break can go unnoticed until `--fetch-timeout` expires. There is no background poll of the server.

Commands inside one repository run one after another. Worktrees that share a git directory are serialized too. `--jobs 1` processes repositories strictly in order. On Linux and macOS a timeout kills the Git process group, SSH included. On Windows the same job is done with `taskkill /T /F`, under its own time limit.

Ctrl+C exits with status 130. Repositories already switched stay switched.

Each log line is text: the local clock (`2026-10-05 14:20:15`), the level, the repository name, and the message. Paths that block an update are listed under that line, one path per line, at most 10. The rest are a single `... and N more` line. `--log-level debug` lists every path and adds fetch and probe detail. Git's own transcript stays quiet. On a terminal the level and the repository name are colored. `NO_COLOR` leaves the text uncolored. Lines from different workers are not interleaved.

A sync creates `<base-dir>/.release-align.lock` and removes it on exit. SIGKILL or a power loss can leave the directory behind. Confirm that the process is gone, then delete that directory. The program never deletes `.git/index.lock`. Other Git clients do not consult this lock, so a second pass over the same clones should not run at the same time.

## Environment

Names match the flags above. Priority and which commands read them are next to the flag table.

| Variable | Flag |
| --- | --- |
| `BASE_DIR` | `--base-dir` |
| `DRY_RUN` | `--dry-run` |
| `LOG_LEVEL` | `--log-level`, `-l` |
| `FETCH_TIMEOUT` | `--fetch-timeout` |
| `PROBE_TIMEOUT` | `--probe-timeout` |
| `LOCAL_TIMEOUT` | `--local-timeout` |
| `CLONE_TIMEOUT` | `--clone-timeout` |
| `ARCHIVE_TIMEOUT` | `--archive-timeout` |
| `JOBS` | `--jobs` |
| `ATTEMPTS` | `--attempts` |
| `RETRY_DELAY` | `--retry-delay` |

The default level is `info`. `LOG_LEVEL` accepts the same names as `--log-level`, in any case.

Git is not asked for a password. Existing credentials and `ssh-agent` are used. If `GIT_SSH_COMMAND` or `GIT_SSH` is set, that value is kept, and the outer timeout still applies. Otherwise OpenSSH is started with `BatchMode=yes`, `ConnectTimeout=5`, one connection attempt, and `StrictHostKeyChecking=accept-new`. A new host key may be written to `known_hosts` on a normal run. Add a passphrase-protected key with `ssh-add` beforehand.

## Building

The code lives in `cmd` and in five packages under `internal`: `app` (the workspace inventory and the sync), `gitlab` (the group project list), `gitter` (running Git and applying process timeouts), `retry` (repeating a check), and `logger` (zap, and writes from several workers).

Inside the retry engine, zero retries means no limit, so `--attempts 1` skips the engine. Canceling the parent context stops retries at once. A timeout of one Git command is retried while that parent context is still alive.

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
