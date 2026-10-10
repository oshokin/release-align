# release-align

release-align prepares the clones under one directory for one release. There is no superproject. `workspace init` finds the repositories already on disk, sync fast-forwards each selected worktree onto its workspace revision, `--remote` and `workspace clone` compare that set with GitLab, and `workspace archive` packs the selected trees into one ZIP.

[West](https://docs.zephyrproject.org/latest/develop/west/manifest.html) already covers the main model: a manifest, shared and per-project revisions, groups, update, and recorded commits. [gita](https://github.com/nosarthur/gita), [repo](https://gerrit.googlesource.com/git-repo/+/HEAD/docs/manifest-format.md), and [mr](https://myrepos.branchable.com/) cover other multi-repo jobs. West is not a weaker tool for managing a set of repositories. These commands are only already fitted to this checkout:

| Task | release-align |
| --- | --- |
| Start from a directory of existing clones | `workspace init` discovers repositories and names groups from parent directories. West starts from a manifest; `west init -l` uses one that already exists. |
| Notice projects GitLab has and the disk does not | `--remote` and `workspace clone` list the saved groups, show what is missing, and clone it. |
| Stop when the shared server does not answer | One probe budget is shared. After it is spent, the run stops instead of retrying inside every repository. |
| Select one directory group | `--group lamiona/search` is the selection. Enabling one west group does not disable the others. |
| Stay on the named release branch | Sync checks out that branch and fast-forwards. `west update` leaves `HEAD` detached at the commit. |
| Hand the selected sources over as one file | `workspace archive` writes one ZIP with the directory layout and a manifest of commits. |

A detached `HEAD` does not change the files in a commit, so it matters when you will commit again, not when you only read the tree. West's `forall` and extensions can add the other steps; keeping them here costs maintenance. That practical gap has not been measured on a real Lamiona checkout.

Module path: `github.com/oshokin/release-align`. Building needs Go 1.27.1. Offline commands set `GIT_NO_LAZY_FETCH=1` on the Git processes they start. Suppression is best effort: a Git build that ignores the variable can still download a missing partial-clone object. That was checked with Git 2.51.1, which honors the variable; the commands also run on Git 2.43.0. A normal sync does not set the variable. Running needs OpenSSH, or your own command in `GIT_SSH_COMMAND` or `GIT_SSH`.

## Quick start

```bash
go build -o release-align .

./release-align workspace init \
  --base-dir "$HOME/go/src/git.example.com" \
  --branch Release-26.3.0 \
  --file ./release-align.yml

# Plan from refs already on disk. No fetch and no writes. Lazy fetch suppression is best effort.
./release-align workspace sync --workspace ./release-align.yml --dry-run

./release-align workspace sync --workspace ./release-align.yml --jobs 4

./release-align --help
./release-align version
```

## Commands

The program takes no positional arguments. With no command it prints help and does not fetch or switch. `workspace sync` and `workspace status` are the alignment commands. `--workspace` is required for a sync or a status check. Flag priority is below the flag table. `help`, `version`, `--version`, and `completion` still run when the environment is invalid, and they do not open repositories. After the flags have been parsed, a bad value or a bad workspace document exits 2 before any repository is touched. A value the parser itself rejects (`--jobs bad`, an unknown flag) is Cobra's own message: stdout is empty and the status is 2. Help and version stay text. When `--output json` is selected, a usage or runtime error of that run is one JSON object on stdout and progress stays on stderr. JSON is not promised before the parser can tell which command is running.

### release-align workspace sync

Prepares the clones named in the workspace file.

```bash
release-align workspace sync --base-dir ~/src --workspace ./release-align.yml --jobs 4 --log-level debug
release-align workspace sync --workspace ./release-align.yml -n -j 8 -l warn
release-align workspace sync --workspace ./release-align.yml --group search --repo storage/pebble-object-bin
```

| Flag | Short | Default | Environment | Meaning |
| --- | --- | --- | --- | --- |
| `--help` | `-h` | | | Print help and exit |
| `--version` | `-v` | | | Print the version line and exit |
| `--base-dir` | | file | `RELEASE_ALIGN_BASE_DIR` | Root that contains the clones. An omitted flag uses `RELEASE_ALIGN_BASE_DIR`, then `release-align.base-dir` from the workspace. `~` and `~/...` expand to the home directory. When the flag, the variable, and the file are all empty, the command exits 2 |
| `--branch` | | `master` | | Replaces `defaults.revision` for this run only when this flag is present. The built-in default does not |
| `--workspace` | | `release-align.yml` | | Workspace YAML (`.yml` or `.yaml`) |
| `--repo` | | | | Select a project by its relative path. Repeatable. Combined with `--group` as a union |
| `--group` | | | | Select a group. Repeatable |
| `--dry-run` | `-n` | `false` | `RELEASE_ALIGN_DRY_RUN` | Plan from refs already on disk. No probe, fetch, switch, or merge |
| `--log-level` | `-l` | `info` | `RELEASE_ALIGN_LOG_LEVEL` | Root flag, inherited by every command. `debug`, `info`, `warn`, or `error`, any case. The workspace key is `release-align.log-level` |
| `--jobs` | `-j` | `4` | `RELEASE_ALIGN_JOBS` | Repositories processed at once, from 1 to 64 |
| `--attempts` | | `3` | `RELEASE_ALIGN_ATTEMPTS` | Origin checks in total, including the first, from 1 to 10. `1` does not start the retry engine |
| `--probe-timeout` | | `5s` | `RELEASE_ALIGN_PROBE_TIMEOUT` | Timeout of one `git ls-remote` |
| `--retry-delay` | | `1s` | `RELEASE_ALIGN_RETRY_DELAY` | Pause between failed origin checks. `0` is allowed |
| `--fetch-timeout` | | `1m` | `RELEASE_ALIGN_FETCH_TIMEOUT` | Timeout of one fetch |
| `--catalog-timeout` | | `30s` | `RELEASE_ALIGN_CATALOG_TIMEOUT` | Timeout of one GitLab projects page |
| `--catalog-budget` | | `3m` | `RELEASE_ALIGN_CATALOG_BUDGET` | Deadline for one GitLab group listing |
| `--local-timeout` | | `40s` | `RELEASE_ALIGN_LOCAL_TIMEOUT` | Timeout of one local Git command |
| `--output` | | `text` | | `text` or `json` |
| `--remote` | | `false` | | After the local operation, compare `gitlab.groups` with the disk and the workspace. Not combined with `--dry-run` |

A duration needs a Go unit: `5s`, `750ms`, `1m`. `RELEASE_ALIGN_DRY_RUN` accepts `1`, `t`, `T`, `true`, `TRUE`, `True`, `0`, `f`, `F`, `false`, `FALSE`, and `False`.

Settings for this program use the `RELEASE_ALIGN_` prefix so they do not collide with global variables. `GITLAB_TOKEN` has no prefix. An explicit flag wins over the environment variable, which wins over the workspace file, which wins over the built-in default. `--log-level` is on the root command, so `workspace stash`, `refresh`, `clone`, `archive`, `init`, `sync`, and `status` all accept it. The file key is `release-align.log-level`. `timeouts` in the workspace follow the same order. When the flag is present, that variable is not parsed. `--base-dir` follows the same order on every command: the flag, then `RELEASE_ALIGN_BASE_DIR`, then `release-align.base-dir` in the workspace file. `workspace init` creates that file, so it stops after `RELEASE_ALIGN_BASE_DIR`. A command that does not define a timeout flag ignores that variable. `workspace init` reads `RELEASE_ALIGN_BASE_DIR` and none of the timeout variables. `workspace refresh` reads only `timeouts.local` from the file. `workspace clone` reads `RELEASE_ALIGN_CLONE_TIMEOUT` (`--clone-timeout`, default `15m`). `workspace archive` reads `RELEASE_ALIGN_ARCHIVE_TIMEOUT` (`--archive-timeout`, default `15m` for one repository). `--branch` has no environment variable. `--retry-delay 0` is allowed. A timeout of `0` is not. Suggested commands are quoted for a POSIX shell, or for PowerShell when the program is running on Windows.

### release-align version

Prints one line and exits 0:

```text
release-align <version> (commit <sha>, built <time>)
```

`task build` stamps a `vMAJOR.MINOR.PATCH` tag only when that commit is the one being built. Any other checkout, and a plain `go build`, print `dev`. `release-align version` and `release-align --version` print the same line. `-v` is this switch. It does not change the log level.

### release-align help

`release-align help`, `--help`, and `-h` print help. `release-align` with no command does the same and does not sync. `release-align help version` and `release-align help completion` print help for those commands. `release-align completion --help` does the same for completion.

### release-align completion

Writes a shell completion script to stdout.

```bash
release-align completion bash
release-align completion zsh
release-align completion fish
release-align completion powershell
```

`--no-descriptions` leaves flag descriptions out of the script. Bash needs the `bash-completion` package. The current bash session can load the script with `source <(release-align completion bash)`.

### release-align workspace status

Reads cached refs and worktrees. Without `--remote` it does not call GitLab. It does not accept `--dry-run`. `--remote` reads the group inventory, including archived projects, before the local check and does not clone. `freshness=cached` means the refs used for readiness were already local; it is not a certificate that every installed Git build stayed off the network.

```bash
release-align workspace status --base-dir "$HOME/src/git.example.com" \
  --workspace ./release-align.yml --group search --output json
```

## Workspace file

`workspace init` writes `release-align.yml` from clones that are already on disk. `--base-dir` is stored as `release-align.base-dir`. An omitted flag uses `RELEASE_ALIGN_BASE_DIR`. Init does not read a workspace file for that path, because it is creating the file. Later commands use the flag, then `RELEASE_ALIGN_BASE_DIR`, then the stored path. There is no built-in checkout path. `--branch` defaults to `master`. `--file` defaults to `release-align.yml` in the current directory and must end in `.yml` or `.yaml`. `--release` is an optional label. `--file` is the workspace, not the report format (`--output`). Project paths stay relative to the stored base directory. `--branch` is stored as `manifest.defaults.revision: refs/heads/<branch>`. Sync and status notice a missing branch, a dirty tree, or a commit that is not local yet. Init reads `RELEASE_ALIGN_BASE_DIR` and does not read the timeout variables of sync. When `--gitlab-url` is omitted and the last component of `--base-dir` is a DNS name, init stores `https://` plus that name in lower case and prints one line saying so. `src`, `work`, `lamiona`, and `localhost` do not qualify. Parent directories are not searched. When `--gitlab-group` is omitted and a GitLab URL is saved, `gitlab.groups` is the first path segment of each origin on that host, sorted and deduplicated. A local folder whose remote uses another namespace is stored under the remote namespace. Each project `url` stays `git remote get-url origin`.

```bash
release-align workspace init \
  --base-dir "$HOME/src/git.example.com" \
  --branch Release-26.3.0 \
  --release 'Lamiona 26.3.0' \
  --file ./release-align.yml
```

A group is the relative path of a parent directory. Every ancestor prefix is recorded. A repository that sits directly under the base directory has no group. `lamiona/search` and `another/search` stay separate, because the group is `lamiona/search`, not the bare name `search`. `--group lamiona` selects everything under that directory. `--group lamiona/search` selects that subgroup. These are folder names, not a guess about which services depend on each other, and not a check of GitLab namespaces.

| Directory under the base | Groups |
| --- | --- |
| `search/calyra` | `search` |
| `lamiona/search/calyra` | `lamiona`, `lamiona/search` |
| `lamiona/storage/pebblebox` | `lamiona`, `lamiona/storage` |
| `standalone` | none |

The scan walks the base directory, skips hidden directories, and does not follow a directory symlink. A `.git` directory or a regular gitfile marks a working tree. A `.git` symlink is rejected. The candidate must be the real repository root. Nested clones inside a found root, including vendor and submodules, are not added. A bare repository is skipped and its object database is not walked. If the base directory itself is a repository, the command stops and asks for the parent directory. A broken `.git` or a read error stops the command. The file is not written. A repository without `origin` is still listed, with an empty URL, and the result names that gap. An empty tree is an error too. A service that was never cloned cannot appear. The file is a starting inventory, not proof that the server has nothing else. Read it, add any missing paths by hand, and keep it.

`ready` describes the selected entries in the file, not every clone under the base directory. A new local clone stays invisible to `status` and sync until it is listed. A listed path that is gone is reported when that path is part of the selection.

The destination is checked before the scan and again before the write. The document is validated, including the 1 MiB limit, before the destination is opened. The file is created with mode `0600` and only if that path does not exist. A symlink at the destination is also refused. There is no `--force`. The parent directory is not created for you. If the write or close fails, the new partial file is removed. An older file is not replaced. A crash can leave a partial new file; reading it fails as bad YAML. To regenerate, pick another filename and diff:

```bash
release-align workspace init --base-dir "$HOME/src/git.example.com" \
  --branch Release-26.3.0 --file ./workspace.candidate.yml
diff -u ./release-align.yml ./workspace.candidate.yml
```

### release-align workspace refresh

`refresh` compares that file with the clones under the saved base directory. `--file` defaults to `release-align.yml`. `--base-dir` overrides the path stored in the file. With no `--add`, no `--sync`, and no `--remote`, the command only prints the difference and leaves the file, its modification time, and every Git worktree unchanged.

```bash
./release-align workspace refresh \
  --base-dir "$HOME/go/src/git.example.com" \
  --file ./release-align.yml

./release-align workspace refresh \
  --base-dir "$HOME/go/src/git.example.com" \
  --file ./release-align.yml \
  --add lamiona/search/new-indexer

./release-align workspace refresh \
  --base-dir "$HOME/go/src/git.example.com" \
  --file ./release-align.yml \
  --sync

./release-align workspace refresh \
  --base-dir "$HOME/go/src/git.example.com" \
  --file ./release-align.yml \
  --sync lamiona/search/old-checkout
```

`--add` is repeatable and is one exact relative path, not a glob. A comma stays inside that path. `--add` and `--sync` cannot be combined. Adding a path that is already listed does nothing. Adding a path that is not a new local clone fails the whole request before the file is replaced. `--add` leaves listed paths that are no longer on disk in the file. `--sync` appends every new local clone and drops every listed path whose directory is confirmed absent. `--sync PATH` limits that diff to the named paths: a missing listed path is dropped, and a new local clone is appended. Other paths stay. Projects that remain keep their order, pins, groups, and comments. A second `--sync` with nothing to add or drop does not open the file for writing. A bare `--sync` refuses to write when the scan finds no clones and every listed path is missing, so an empty base directory cannot empty the file.

New entries go after the existing ones, in lexical order, with directory groups from the same rules as `init` and with no revision of their own. They inherit `manifest.defaults.revision`. When that field is omitted, west's `master` is the inherited revision. Existing release label, defaults, groups, pins, comments, and project order stay. A no-op refresh does not rewrite the file. A real write keeps the other YAML nodes and may reflow the appended project.

An unlisted clone is left for `--add` or `--sync`. Another project under the same directory may have been left out on purpose. A preview reports a missing listed path and leaves it in the file. `--add` leaves it there. `--sync` drops every missing path. `--sync PATH` drops that path. A listed path that exists but is not a usable repository, including a broken `.git`, a missing origin, a symlink, or a permissions error, stops the command and does not write. A hidden or nested root that you put in the file by hand is checked directly, so the directory walk skipping it does not make it look deleted.

Refresh does not fetch, switch, or ask the server whether `defaults.revision` exists. Exit 0 means the comparison finished. Unlisted and missing paths can still be present. Exit 1 is a scan, Git, lock, or write failure. Exit 2 is flags or a workspace document that cannot be updated. The output does not say that the repositories are ready.

A repository that was created on the server but never cloned is not in the disk scan. `workspace status --remote` and `workspace clone` read that list from the GitLab groups saved in the file. Disk `refresh` does not call the API.

`--remote` compares listed paths with the GitLab catalog, including archived projects. A path the catalog does not return is printed. A project GitLab now reports as archived or active is printed separately and is not treated as absent. A path outside the saved GitLab groups stays. Without `--sync` or `--apply`, nothing is written. `--remote --sync` drops the absent paths from the file and leaves every directory on disk. It does not append local clones and it does not store archive marks. `--remote --apply` stores those marks for projects already in the file and does not drop paths or change pins. When both kinds of difference are present, the preview names both actions. `--remote` cannot be combined with `--add` or with path arguments. `--apply` cannot be combined with `--sync` or `--delete`. A successful catalog of zero projects does not empty a non-empty file.

`--delete` is allowed only together with `--remote --sync`. It removes the directories that this same run just dropped, and no others. An unlisted clone is left in place. Deleting a directory is not part of disk `--sync`.

While it writes, refresh creates `<file>.lock` next to the workspace with `O_EXCL` and does not wait. The lock coordinates release-align processes. An editor does not take it. If a crash leaves the lock, confirm that no refresh is running, then delete that file. The program does not delete a lock it did not create and does not treat an old lock as free. A preview does not create the lock.

The replacement is a temporary file in the same directory, renamed onto the workspace name. On Unix that rename replaces the directory entry. Go does not promise the same atomicity on other operating systems, and this is not a crash-proof replace on every OS. If the rename fails, the original file stays and there is no second, destructive replace. Before the rename, refresh checks that the path is still that regular file and that its bytes are still the bytes it read. The check can notice an outside edit. It is not an atomic compare-and-swap against an editor, and two hard links to the file are not a supported way to edit it.

### release-align workspace stash

`stash` runs `git stash push -u` in each dirty repository named by the workspace file. A clean worktree is skipped. A missing directory is reported and skipped. Every listed repository gets one progress line. A clean worktree is INFO. A new stash, an existing stash, and a missing directory are WARN. The line has the same shape as `status`: the path, `phase=stash`, and the stash oid when one was created or already present. The last line says `finished` and `done=N/N`.

The stash message is `release-align`. A second run does not create another stash when one with that message already exists, and it leaves newer uncommitted changes in the worktree. The command does not pop. Pop one repository at a time after you have inspected it. `workspace sync` still refuses a dirty tree, so stash first, then sync. Commits that are already on a branch stay on that branch.

```bash
./release-align workspace stash \
  --base-dir "$HOME/go/src/git.example.com" \
  --file ./release-align.yml
```

A project revision is `refs/heads/<branch>`, `refs/tags/<tag>`, or a full SHA-1 or SHA-256 (the writer quotes commits). If it is omitted, `manifest.defaults.revision` is used. `--branch` replaces that default only for the current run and does not remove a pin, even when the pin equals the default. A short name such as `main` is accepted when local refs identify it as a branch (`refs/heads/main` or `refs/remotes/origin/main`) or a tag (`refs/tags/main`). A name that is both a branch and a tag is rejected. A short name that is not in the local refs yet is rejected with a request to write `refs/heads/<name>` or `refs/tags/<name>`; the tool does not guess after a later fetch. A branch that exists both locally and as `origin/<branch>` is normal. Only selected projects need this lookup; `refresh` and `clone` inventory operations do not resolve revisions. The branch resolves to `origin/<branch>`, the tag to its peeled commit, and the commit to that object. Another branch, another tag, and the current upstream are not substitutes.

A dirty tree, an unfinished merge or rebase, or a detached HEAD that is not already the pinned commit is reported and left untouched. A different local branch is left in place, including commits that were never pushed, and HEAD moves to the pinned revision. Unpushed commits on the pinned branch itself stay where they are. Local commits and uncommitted edits are not carried forward and are not discarded.

Sync checks origin (`--attempts 3` is three tries total), fetches the selected clones, resolves every target, and switches nothing if any selected project is already blocked. `--ignore-errors` (`RELEASE_ALIGN_IGNORE_ERRORS`) checks out every repository that can move and leaves the blocked ones unchanged. The exit status is still 3 when any selected repository is not ready. A successful probe is remembered for that host only. A permission failure is recorded on the repository that was checked; the next repository on the same host is still probed. If one selected repository is blocked, the others are not checked out, unless `--ignore-errors` is set. A repository that answered is `plan_blocked`, not a copy of the first failure. A network failure still stops the run after the shared attempt budget.

The fast-forward then uses that resolved commit. Afterward the worktree is read again. `ready` comes from that read. If switch or the fast-forward fails, the worktree is read once more and that later read is `actual`. When the read cannot be done, or the run is already canceled, `actual` is omitted. The branch from before the switch is not reported as current. There is no rollback.

The update itself is a local `git merge --ff-only` from the resolved commit. There is no second network request, no merge commit, no rebase, and no push. A tag that exists on origin replaces the local tag of the same name. A tag that exists only in the clone is kept. Git still runs hooks and filters, under the same command timeout. Recursive submodule checkout and fetch are off. `switch` and the fast-forward pass `--no-overwrite-ignore`, so a local ignored file is left in place.

Fetch may update remote-tracking refs even when the worktrees stay put; the report then has `freshness` `fetched`. `status` and `--dry-run` do not fetch (`freshness` `cached`) and set `GIT_NO_LAZY_FETCH=1`, as described above. A dry-run exits 0 when the cached plan has no blocker, and `ready` is still false. `--dry-run --remote` is rejected; use `workspace status --remote` to ask GitLab. URL userinfo and the credential query parameters `token`, `access_token`, `private_token`, `password`, `oauth_token`, and `secret` are removed from Git diagnostics before they are logged or printed. That does not cover an arbitrary secret from a hook or a credential helper.

`--output json` writes one JSON object to stdout. Progress and logs go to stderr. `expected_count` is the selection, `inventory_count` is the whole file, and `scope` is `workspace` or `selection`. `coverage_complete` means every selected path has a row, not that every clone exists.

| Exit | Meaning |
| --- | --- |
| 0 | Every selected project matches, or a dry-run plan has no blocker |
| 1 | Git, network, access, or I/O failure, including a JSON write error. Also a requested GitLab inventory that could not be read, even when the selected branches were already switched |
| 2 | Flags or the workspace document were rejected before fetch |
| 3 | The selection is not ready |
| 130 | Ctrl+C |

`status` cannot see commits that are only on the server. A workspace file may name full commit IDs directly. This build does not write a freeze file and does not add worktrees. Missing clones are not created by `status` or `sync`. Nothing is pushed, tagged, or built.

The workspace is one YAML document: a west `manifest` and a `release-align` block. `manifest.projects[].path` is still the `--repo` value. `name` is a short unique west name and is not the selector. `release-align.gitlab` is the server scope for `--remote` and `workspace clone`. `gitlab.groups` are GitLab namespace paths, including subgroups. `projects[].groups` stay the local selection labels. One is not derived from the other. `clone-protocol` is `ssh` or `https`; an empty value means `ssh`. The API token is `GITLAB_TOKEN` in the environment, sent as `Private-Token`. It is not stored in the file, the command line, or a clone URL. `release-align.timeouts` stores only the overrides you set (`probe`, `fetch`, `catalog`, `catalog-budget`, `local`, `clone`, `archive`). Omitted fields keep the program default. An empty string, `0`, a negative duration, or a bare number is rejected. `workspace init --gitlab-url` saves that origin as given. Without the flag, a DNS-shaped `--base-dir` name is saved instead. Repeatable `--gitlab-group` replaces that guess. Init does not call the API.

This is not a replacement for west. A flat manifest with projects, URLs, paths, groups, and unambiguous branch, tag, or commit revisions can be passed to `--workspace` without moving the clones. `import` and enabled submodule updates are rejected before Git changes anything; `submodules: false` is accepted. `clone-depth` is preserved as metadata, but `workspace clone` refuses to use it. Resolve imports with `west manifest --resolve` and pass that file. `self.path` is not `--base-dir`. `west-commands` are kept and never executed. `--output json` and `_release-align/manifest.json` inside an archive stay JSON. Anchors, aliases, merge keys, and custom YAML tags are rejected. New files use two-space indentation and compact group lists. Only configured timeout overrides are written. `manifest.version` is a west schema version; `release-align.schema-version` is our own document version. Unsupported west schema versions fail explicitly. `manifest.group-filter` retains inactive projects in the file while excluding them from an unfiltered run. Explicitly selecting an inactive project or a group with no active projects is an error. External `.west/config` settings are not inherited. release-align always clones with remote name `origin`, even when Git's `clone.defaultRemoteName` is different. A west checkout that uses another remote name is `missing_origin` until you add `origin` yourself after checking `git remote -v`. A detached HEAD is left detached. West ignores `userdata`, so `west update` does not honor an archive mark stored there.

An archived GitLab project is read-only on the server and stays in the catalog. The mark is saved as `userdata.release-align.archived` and does not remove the project, its groups, or its pin. Sync and status skip it before fetch and checkout. A missing archived clone does not make the active selection fail. `workspace clone` downloads an archived project only with `--include-archived`, and a later clone does not fetch an archived checkout that is already on disk. `workspace refresh --remote` shows lifecycle differences. `workspace refresh --remote --apply` writes those marks for projects already in the file and does not change pins or directories. Without `--remote`, sync uses the saved marks and says the GitLab lifecycle was not checked. A ZIP archive of an archived project is allowed: it reads the requested revision locally and does not replace that revision with HEAD. The ZIP is a `git archive` of committed trees. It is not a full backup: uncommitted files, export-ignore paths, submodules, and Git LFS payloads are not a build mirror.

When a project URL is set, status, sync, and archive compare it with `origin`. Host case is ignored, the namespace case is kept, and a trailing `.git` is ignored. A different repository is `remote_mismatch`. The origin URL is not rewritten. A URL that cannot be compared is reported as unchecked identity, not as a match. `url.*.insteadOf` is transport configuration and is not treated as a different service being the same repository.

```bash
release-align workspace status --workspace ./release-align.yml --base-dir "$RELEASE_ALIGN_BASE_DIR" --remote

release-align workspace clone \
  --workspace ./release-align.yml \
  --base-dir "$RELEASE_ALIGN_BASE_DIR" \
  --repo lamiona/search/new-indexer

release-align workspace clone \
  --workspace ./release-align.yml \
  --base-dir "$RELEASE_ALIGN_BASE_DIR" \
  --all
```

`workspace archive` writes one ZIP of the selected trees. `--file` is resolved from the current directory. Paths inside the archive follow `projects[].path`, for example `lamiona/search/calyra/go.mod`, plus `_release-align/manifest.json` with the resolved commit for each repository. Each project path must be the worktree root; a subdirectory of another clone is rejected. Omit `--repo` and `--group` to pack every repository. The revision is the pin, or `defaults.revision` when there is no pin. Those names are resolved to commits from local refs before the first `git archive`. A local branch or the current HEAD is not substituted. There is no fetch, clone, or checkout. A missing clone or object stops the command, and the destination file is not created or replaced. `git archive` exports the committed tree, so uncommitted and untracked files stay out. Tracked files stay unless `.gitattributes` marks them `export-ignore`. Submodule contents are not downloaded; gitlinks are listed in the manifest. The archive is not a byte-identical promise and it is not a full build backup.

`--remote` on `status` or on a sync reads the saved groups before checkout and prints the difference, including a change from active to archived. It does not clone and it does not change the workspace file. Those marks are written by `refresh --remote --apply`. Without `--remote` the report says the inventory was not checked. `ready` counts selected active rows. Archived rows in the selection are a separate count and are not part of that denominator. Uncloned projects in the GitLab scope are a separate list, not a failed selection. Archived projects stay in the catalog. Projects shared in from outside the group are omitted. If the API does not return a complete list, the report says the catalog is unknown and does not claim that nothing is new. Checkout does not start, and the exit status is 1. A later Git network failure or a cancel does not ask GitLab again, because the catalog was already read.

`workspace clone` downloads missing checkouts into `<base-dir>/<path_with_namespace>`, reuses a matching checkout, and appends new rows without changing pins or order. `--repo` and `--all` cannot be combined. `--all` is the saved server scope, not the whole GitLab instance and not `--group`. A project with no default branch is skipped by `--all` and rejected by `--repo`. An occupied path or a different origin is left alone. Clones run one after another, on the remote default branch, without a depth filter. A later `workspace sync` aligns the release. If one clone fails, finished checkouts stay on disk and completed new rows are still saved. If the YAML write fails, those checkouts stay and the output includes a recovery command. Ctrl-C does not start that write. Clone takes the base-directory lock before the workspace file lock described above.

## Network and parallelism

With three attempts, a 5s probe, and a 1s pause, preflight is bounded by 17s plus the time taken to stop child processes. That figure is only the preflight budget, not a bound on fetch or checkout. Fetch and checkout wait for a successful preflight.

Network errors and timeouts are retried. A rejected key, a permission failure, a missing repository, and an untrusted TLS certificate fail that one repository. Probe, fetch, and planning still run for the other selected repositories. Checkout of the rest does not start when any selected repository is already blocked. Git is started with `LC_ALL=C`, and the error text is what classifies the failure. An unrecognized error does not start an open-ended retry loop.

If the network drops after preflight, a failed fetch checks origin again. Three network failures cancel the repositories still waiting and the Git processes still running. The checks are serialized, so once the outage is confirmed a worker does not run a retry loop of its own. A successful check allows one more fetch. While a fetch is still running, the break can go unnoticed until `--fetch-timeout` expires. There is no background poll of the server.

Commands inside one repository run one after another. Worktrees that share a git directory are serialized too. `--jobs 1` processes repositories strictly in order. On Linux and macOS a timeout kills the Git process group, SSH included. On Windows the same job is done with `taskkill /T /F`, under its own time limit.

Ctrl+C exits with status 130. Repositories already switched stay switched.

Each log line is text: the local clock (`2026-10-05 14:20:15`), the level, the repository name, and the message. Fields after the message are `key=value` pairs separated by commas. A phase line also carries `done` or `found`, `percent`, `elapsed`, and `left` (an estimate from the average so far; `unknown` until the first unit finishes). Paths that block an update are listed under that line, one path per line, at most 10. The rest are a single `... and N more` line. `--log-level debug` lists every path and adds fetch and probe detail. Git's own transcript stays quiet. On a terminal the clock, the level, the repository name, and the `elapsed`, `left`, and `percent` values are colored. `NO_COLOR` leaves the text uncolored. Lines from different workers are not interleaved. Repositories left untouched by Ctrl+C, a dead origin, or a blocked plan are one `count` line, not one line each. A Git error is one line: later stderr lines follow the first, separated by semicolons.

A sync creates `<base-dir>/.release-align.lock` and removes it on exit. SIGKILL or a power loss can leave the directory behind. Confirm that the process is gone, then delete that directory. The program never deletes `.git/index.lock`. Other Git clients do not consult this lock, so a second pass over the same clones should not run at the same time.

## Environment

Program settings use the `RELEASE_ALIGN_` prefix. `GITLAB_TOKEN` does not: it is the container credential for the GitLab API. Priority and which commands read them are next to the flag table.

| Variable | Flag |
| --- | --- |
| `RELEASE_ALIGN_BASE_DIR` | `--base-dir` |
| `RELEASE_ALIGN_DRY_RUN` | `--dry-run` |
| `RELEASE_ALIGN_LOG_LEVEL` | `--log-level`, `-l` |
| `RELEASE_ALIGN_FETCH_TIMEOUT` | `--fetch-timeout` |
| `RELEASE_ALIGN_CATALOG_TIMEOUT` | `--catalog-timeout` |
| `RELEASE_ALIGN_CATALOG_BUDGET` | `--catalog-budget` |
| `RELEASE_ALIGN_PROBE_TIMEOUT` | `--probe-timeout` |
| `RELEASE_ALIGN_LOCAL_TIMEOUT` | `--local-timeout` |
| `RELEASE_ALIGN_CLONE_TIMEOUT` | `--clone-timeout` |
| `RELEASE_ALIGN_ARCHIVE_TIMEOUT` | `--archive-timeout` |
| `RELEASE_ALIGN_JOBS` | `--jobs` |
| `RELEASE_ALIGN_ATTEMPTS` | `--attempts` |
| `RELEASE_ALIGN_RETRY_DELAY` | `--retry-delay` |
| `GITLAB_TOKEN` | |

The default level is `info`. `RELEASE_ALIGN_LOG_LEVEL` accepts the same names as `--log-level`, in any case.

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
