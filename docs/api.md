# Lua API

A bull config is a plain Lua file. The API has two parts:

- **Config globals** for writing configs: `host`, `env`, `secret`, and the
  resource types (`file`, `shell`, plus anything from `bull_modules`).
- **The `bull` table** for writing your own resource types:
  `bull.resource`, `bull.ssh.*`, `bull.exec`, and helpers.

Relative local paths (`key`, `local_path`, `compose_file`, `env_file`,
`bull.read_file`, …) resolve against the directory of the config file.

## Config globals

### `host(name, opts)`

Declares an SSH host that resources refer to by `name`.

| opt                | description                                              |
|--------------------|----------------------------------------------------------|
| `addr`             | `host:port`                                              |
| `user`             | SSH user                                                 |
| `password`         | password auth                                            |
| `key` / `key_file` | path to a private key                                    |
| `insecure`         | `true` skips host key verification (test containers only) |

Host keys are checked against `~/.ssh/known_hosts` unless `insecure = true`.

```lua
host("web", { addr = "10.0.0.5:22", user = "root", key = "~/.ssh/id_ed25519" })
```

### `env(name [, default])`

Returns the environment variable `name`. Raises an error if it is unset or
empty and no default is given.

### `secret(name [, default])`

Returns a secret from the encrypted secrets file (`.bull.secrets.enc`, managed
with `bull secrets`). Raises an error if it is missing and no default is given.
Every value returned by `secret()`, defaults included, is redacted in plan
output.

```lua
docker_compose("app", { host = "web", dir = "/opt/app", compose = "...",
  env = { DB_PASSWORD = secret("db_password") } })
```

### Common resource options

Every built-in resource type takes `(id, opts)`. The `id` must be unique
across the config. Unknown options are an error.

| opt          | description                                                        |
|--------------|--------------------------------------------------------------------|
| `host`       | (required) name of a declared host                                 |
| `depends_on` | list of resource ids that are applied first; if one fails, this one is skipped |

## Built-in resource types

### `file(id, opts)`

Ensures a file on the host has the given content and mode.

| opt                     | description                              |
|-------------------------|------------------------------------------|
| `path` / `remote_path`  | (required) remote path                   |
| `content`               | inline content                           |
| `local_path` / `src`    | read the content from a local file       |
| `mode`                  | octal string, default `"0644"`           |

The plan shows a line diff of the content. Files over 512 KiB are compared
by hash only.

### `shell(id, opts)`

Runs a command on the host.

| opt                          | description                                         |
|------------------------------|-----------------------------------------------------|
| `command` / `cmd` / `run`    | (required) shell command                            |
| `creates`                    | remote path; skip the command if it already exists  |
| `unless`                     | shell check; skip the command if it exits 0         |

If neither guard is set, the command runs on every apply.

## Example modules

These live in [`test/config/bull_modules/`](../test/config/bull_modules) and
are not built in. Copy the ones you need into your own `bull_modules/` and
adapt them: `docker_compose`, `apt`, `apk`, `hetzner_dns`.

### `docker_compose(id, opts)`

Uploads a compose project and runs `docker compose up -d --remove-orphans --pull always`.

| opt                      | description                                       |
|--------------------------|---------------------------------------------------|
| `dir` / `directory`      | (required) remote project directory               |
| `compose`                | inline compose YAML                               |
| `compose_file` / `file`  | local compose file                                |
| `env`                    | table of `KEY = value`, written to `.env`         |
| `env_file`               | local `.env` file                                 |

One of `compose` or `compose_file` is required.

The module pulls images on every apply (`--pull always`) and removes orphaned
containers. Edit your copy if you want different behavior.

## Writing resource types

A resource type is just a Lua function that validates its options and calls
`bull.resource`. Put it in `bull_modules/<name>.lua` next to your config. Every
`*.lua` file in that directory is loaded automatically in alphabetical order
before the config runs, and `require("<name>")` resolves from there too.

A module or config may redefine an existing global, e.g. to replace the
built-in `file` with your own. Bull allows it but prints a warning such as
`warning: bull_modules/file.lua overrides "file" from built-in`, so accidental
overrides (two modules both defining `apt`) don't go unnoticed.

### `bull.resource(id, def)`

Registers a resource with the engine.

| field        | description                                                     |
|--------------|-----------------------------------------------------------------|
| `type`       | type name, shown as `<type>.<id>` in output                     |
| `depends_on` | list of resource ids                                            |
| `plan`       | `function() → result` — inspects current state, must not change anything |
| `apply`      | `function()` — makes the change; raise an error with `error()` on failure |

`apply` is only called when `plan` reported a change.

#### Plan result

```lua
{
  change = "create" | "update" | "delete" | "no-op",
  diffs  = {
    { field = "version", before = "1.0", after = "1.1" },
    { field = "content", before = old, after = new, text = true },
  },
  error  = "optional message; marks the plan as failed",
}
```

- Omit `before` for new values and `after` for removed ones.
- `text = true` renders `before`/`after` as a line diff (for file contents).
- Raising an error inside `plan` has the same effect as returning `error`.

### `bull.ssh.exec(host, cmd) → output, exit_code, err`

Runs `cmd` on `host`. `output` is stdout and stderr combined, with trailing
newlines trimmed. `err` is non-empty (and `exit_code` is `-1`) only when the
connection itself failed. A non-zero exit code is not an error.

```lua
local out, code, err = bull.ssh.exec(h, "test -e " .. bull.q(path))
if err ~= "" then error("cannot reach host '" .. h .. "': " .. err) end
```

### `bull.ssh.upload(host, path, content [, mode]) → err`

Writes `content` to `path` on `host`. `mode` is a number, default `0644`
(`420`). Returns `""` on success.

### `bull.ssh.read(host, path) → content, err`

Reads a remote file. Unlike `bull.ssh.exec`, the content is returned untrimmed.

### `bull.exec(cmd) → stdout, exit_code, err`

Runs `cmd` locally with `sh -c`. Same return contract as `bull.ssh.exec`,
except only stdout is captured. Useful for calling HTTP APIs with `curl`.

### `bull.q(str) → string`

Single-quotes `str` for safe use in a shell command. Quote every value you
interpolate into `bull.ssh.exec` / `bull.exec`.

### `bull.read_file(path) → content`

Reads a local file. Raises an error if it can't be read.

### `bull.sha256(str) → hex`

Returns the SHA-256 of `str` as a hex string.

### `bull.json.encode(value) → str, err`

### `bull.json.decode(str) → value, err`

On failure the first return value is `nil` and `err` holds the message.

### `_validate_opts(type, id, opts, known_keys)`

Raises an error if `opts` contains keys not listed in `known_keys`. Include
`"depends_on"` in the list.

### Example

```lua
-- bull_modules/apt.lua
function apt(id, opts)
  _validate_opts("apt", id, opts, { "host", "packages", "depends_on" })
  local h = opts.host

  local function missing()
    local out, _, err = bull.ssh.exec(h,
      "dpkg-query -W -f '${Package} ${db:Status-Abbrev}\\n' 2>/dev/null; true")
    if err ~= "" then error("cannot reach host '" .. h .. "': " .. err) end
    local installed, list = {}, {}
    for pkg, status in out:gmatch("(%S+) (%S+)") do
      if status:sub(1, 2) == "ii" then installed[pkg] = true end
    end
    for _, pkg in ipairs(opts.packages) do
      if not installed[pkg] then table.insert(list, pkg) end
    end
    return list
  end

  bull.resource(id, {
    type = "apt",
    depends_on = opts.depends_on,
    plan = function()
      local m = missing()
      if #m == 0 then return { change = "no-op" } end
      return { change = "update",
        diffs = { { field = "packages", after = table.concat(m, ", ") } } }
    end,
    apply = function()
      local quoted = {}
      for i, pkg in ipairs(missing()) do quoted[i] = bull.q(pkg) end
      local out, code, err = bull.ssh.exec(h,
        "DEBIAN_FRONTEND=noninteractive apt-get install -y " .. table.concat(quoted, " ") .. " 2>&1")
      if err ~= "" then error(err) end
      if code ~= 0 then error("apt-get install failed:\n" .. out) end
    end,
  })
end
```

See `test/config/bull_modules/` for more complete modules: `docker_compose.lua`
for a multi-file resource, and `hetzner_dns.lua`, which talks to an HTTP API
through `bull.exec` and `curl`.
