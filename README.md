# bull

Stupid simple infrastructure management with Lua.

## Why

A lightweight alternative to Ansible and Terraform, taking the good parts of both:

- **From Ansible:** push config over plain SSH, no agents. Deploying Docker Compose projects to a few hosts is the main use case.
- **From Terraform:** `bull plan` shows exactly what will change before anything is applied. When you bump a version, you see which stacks it touches instead of guessing or running everything.
- **Easy:** a single binary and plain Lua configs. Resources are small Lua modules, simple enough to write or extend with AI.
- **Fast:** a full plan should take seconds, so you never need tags or partial runs just to save time.

## Features

- Plan with readable diffs before changes
- Apply in dependency order
- Module-style resources, e.g. `file`, `apt`, `apk`, `docker_compose`
- `bull_modules` autoloaded from your config directory

See [docs/api.md](docs/api.md) for the full Lua API and how to write your own resource types.

## Example Lua config

```lua
host("test", {
  addr = "127.0.0.1:2222",
  user = "root",
  password = "root",
})

file("myapp-config", {
  host    = "test",
  path    = "/opt/myapp/nginx/default.conf",
  content = [[
server {
  listen 80;
  location / {
    return 200 'moin bull';
    add_header Content-Type text/plain;
  }
}
]],
})

apt("base-packages", {
  host     = "test",
  packages = { "openssh-server", "curl" },
  update   = true,
})

docker_compose("myapp", {
  host = "test",
  dir  = "/opt/myapp",
  compose = [[
services:
  app:
    image: nginx:alpine
    restart: unless-stopped
    ports:
      - "8080:80"
    volumes:
      - ./nginx/default.conf:/etc/nginx/conf.d/default.conf:ro
]],
})
```

## Example output

### bull plan infra.lua

Changed files are shown as line diffs, with secret values redacted. New files
are previewed up to 30 lines and files over 512 KiB are compared by hash only;
pass `--full-diff` to show everything.

```text
Plan

  ~ file.myapp-config
    └─ content  +1 -1
         @@ -1,5 +1,5 @@
          server {
         -  listen 80;
         +  listen 8080;
            location / {
              return 200 'moin bull';
              add_header Content-Type text/plain;

  + apt.base-packages
    └─ install  curl

  ~ docker_compose.myapp
    ├─ compose  +1 -1
    │    @@ -1,4 +1,4 @@
    │     services:
    │       app:
    │    -    image: nginx:1.27-alpine
    │    +    image: nginx:1.29-alpine
    │         restart: unless-stopped
    └─ env      +1 -1
         @@ -1,2 +1,2 @@
          DB_PASSWORD=[REDACTED]
         -VERSION=1.27-alpine
         +VERSION=1.29-alpine

  ────────────────────────────────────────────────────
  1 to create  ·  2 to update  ·  0 to delete  ·  0 unchanged
```

### bull apply infra.lua

Prints the same plan, then asks for confirmation:

```text
Apply 3 change(s)? [y/N]: y
  Updating  file.myapp-config
  ✓  file.myapp-config

  Creating  apt.base-packages
  ✓  apt.base-packages

  Updating  docker_compose.myapp
  ✓  docker_compose.myapp
```
