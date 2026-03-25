-- test/infra.lua
-- Integration test against the local SSH container.
-- Start the container first: docker compose up -d  (from the test/ directory)

host("test", {
  addr     = "localhost:2223",
  user     = "root",
  key      = "~/.ssh/id_ed25519",
  insecure = true,  -- skip host key check for the test container
})

-- Shell: runs once because of creates guard
shell("create-dir", {
  host    = "test",
  command = "mkdir -p /opt/myapp && touch /opt/myapp/.init",
  creates = "/opt/myapp/.init",
})

-- Shell: always runs (no guard) — idempotent by nature
shell("write-motd", {
  host    = "test",
  command = "echo 'managed by bull' > /etc/motd",
  unless  = "grep -q 'managed by bull' /etc/motd 2>/dev/null",
})

-- File upload
file("config", {
  host    = "test",
  path    = "/opt/myapp/config.txt",
  content = "hello from bull\n",
})

file("moin", {
  host    = "test",
  path    = "/opt/myapp/moin.txt",
  content = "moin bull\n",
})

-- apt: openssh-server is already installed, curl is not
apt("base-packages", {
  host     = "test",
  packages = { "openssh-server", "curl" },
})

-- Docker Compose stack (docker is shimmed to echo)
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
]],
})

-- Reusable component — plain Lua function
function webapp(name, opts)
  shell(name .. "-dir", {
    host    = opts.host,
    command = "mkdir -p /opt/" .. name,
    creates = "/opt/" .. name,
  })
  docker_compose(name, {
    host    = opts.host,
    dir     = "/opt/" .. name,
    compose = opts.compose,
    depends = { name .. "-dir1" },
  })
end

webapp("blog", {
  host = "test",
  compose = [[
services:
  blog:
    image: ghost:alpine
    restart: unless-stopped
]],
})
