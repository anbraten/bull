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

```text
Plan

  ~ file.myapp-config
    ~ path: /opt/myapp/nginx/default.conf
    ~ content: old nginx config -> moin bull config

  ~ apt.base-packages
      + package curl

  + docker_compose.myapp
    + service app with mounted nginx config

  1 to create  2 to update  0 unchanged  0 errors
```

### bull apply infra.lua

```text
Plan

  ~ file.myapp-config
    ~ path: /opt/myapp/nginx/default.conf
    ~ content: old nginx config -> moin bull config

  ~ apt.base-packages
      + package curl

  + docker_compose.myapp
    + service app with mounted nginx config

  1 to create  2 to update  0 unchanged  0 errors

Apply 3 change(s)? [y/N]: y
  Updating file.moin...
  ✓ file.moin
  Updating apt.base-packages...
  ✓ apt.base-packages
  Creating docker_compose.myapp...
  ✓ docker_compose.myapp
```
