# Afribase CLI

Manage Afribase projects, databases, functions and deployments from the terminal.

## Install

```bash
# Homebrew (once the tap is published)
brew install afribase/tap/afribase

# Go
go install github.com/afribase/cli@latest

# From source
git clone https://github.com/Afribase-Dev/afribase-cli
cd afribase-cli && make install
```

Verify with `afribase version`.

## Getting started

```bash
afribase login                       # email + password, saved to ~/.afribase/config.yaml
afribase projects list
afribase link <projectId>            # every command below then defaults to it
```

`--project` overrides the linked project on any command.

## Deploying an app

```bash
# Build from a repository
afribase apps create --name api --repo https://github.com/me/api --branch main

# Ship it, and wait for the result
afribase apps deploy --app api --wait

afribase apps logs --app api --tail 200
afribase apps env set --app api --key STRIPE_KEY --value sk_live_... --secret
afribase apps deploys --app api
afribase apps rollback --app api --deploy <deployId>
```

`--wait` polls until the deploy is `live`, `failed` or `canceled`, and exits
non-zero on failure, so a CI step fails when the deploy does:

```yaml
- run: afribase apps deploy --app api --wait
  env:
    AFRIBASE_ACCESS_TOKEN: ${{ secrets.AFRIBASE_TOKEN }}
```

Any config key can be supplied as an environment variable instead of the config
file: `AFRIBASE_ACCESS_TOKEN`, `AFRIBASE_API_URL`, `AFRIBASE_PROJECT_ID`.

## Commands

| Command | What it does |
| --- | --- |
| `login` / `logout` | Authenticate; credentials in `~/.afribase/config.yaml` (mode 0600) |
| `link <projectId>` | Set the default project for every other command |
| `projects` | `list`, `create`, `delete` |
| `apps` | `list`, `create`, `deploy`, `deploys`, `rollback`, `logs`, `env`, `delete` |
| `db` | `push` a migration, `migrations` to list, `rollback` |
| `functions` | `list`, `deploy`, `delete` edge functions |
| `env` | Project-level config store: `list`, `set`, `delete` |
| `jobs` | `list`, `enqueue`, `retry`, `cancel` background jobs |
| `ai` | `build` — describe a change and review the plan before applying |
| `version` | Version, commit and build date |

## Configuration

`~/.afribase/config.yaml`:

```yaml
access_token: ...
api_url: https://api.useafribase.app
project_id: ...
```

Point at a local stack with `api_url`, or `AFRIBASE_API_URL=http://localhost:8080`.

## Releasing

```bash
make snapshot                                   # build all platforms, publish nothing
git tag -a cli/v0.1.0 -m "CLI v0.1.0"
git push origin cli/v0.1.0
make release                                    # needs GITHUB_TOKEN
```

## Not covered yet

Domains, storage buckets, vector search and build-log streaming are available in
the API but have no CLI commands.
