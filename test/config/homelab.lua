-- homelab.lua
-- Homelab infrastructure managed with bull.
-- Mirrors the Ansible playbook for b5-nas and hetzner04.
--
-- Usage:
--   bull plan  homelab.lua
--   bull apply homelab.lua
--
-- Secrets (add via: bull secrets edit homelab.lua):
--   nextcloud_mysql_root_password, nextcloud_mysql_password,
--   nextcloud_admin_password, nextcloud_redis_password,
--   immich_database_password, papra_auth_secret

-- ── Secrets ───────────────────────────────────────────────────────────────────
-- Set real values via: bull secrets edit homelab.lua --secret-key <key>
-- Default values below are placeholders for local testing only.

local nc_mysql_root_pw = secret("nextcloud_mysql_root_password", "changeme")
local nc_mysql_pw      = secret("nextcloud_mysql_password",      "changeme")
local nc_admin_pw      = secret("nextcloud_admin_password",      "changeme")
local nc_redis_pw      = secret("nextcloud_redis_password",      "changeme")
local immich_db_pw     = secret("immich_database_password",      "changeme")
local papra_secret     = secret("papra_auth_secret",             "changeme")

-- ── Hosts ─────────────────────────────────────────────────────────────────────

host("b5-nas", {
  addr     = "localhost:2223",  -- test container; replace with real address
  user     = "root",
  key      = ".ssh/id_ed25519",
  insecure = true,
})

host("hetzner04", {
  addr     = "localhost:2223",  -- test container; replace with real address
  user     = "root",
  key      = ".ssh/id_ed25519",
  insecure = true,
})

-- ── Components (Ansible roles as Lua functions) ───────────────────────────────

-- common: base packages on any host (mirrors the common role)
function common(h)
  apt("common-" .. h, {
    host     = h,
    packages = {
      "curl", "git", "vim", "htop", "ca-certificates",
      "gnupg", "lsb-release", "unattended-upgrades",
    },
    update = true,
  })
end

-- docker: install docker engine from the official install script
function docker(h)
  shell("docker-install-" .. h, {
    host       = h,
    command    = "curl -fsSL https://get.docker.com | sh",
    unless     = "docker version 2>/dev/null",
    depends_on = { "common-" .. h },
  })
end

-- docker_prune: weekly cleanup cron job (mirrors the docker_prune role)
function docker_prune(h)
  shell("docker-prune-" .. h, {
    host       = h,
    command    = "echo '0 3 * * 0 root /usr/bin/docker system prune -af --volumes"
                 .. " >> /var/log/docker-prune.log 2>&1' > /etc/cron.d/docker-prune",
    creates    = "/etc/cron.d/docker-prune",
    depends_on = { "docker-install-" .. h },
  })
end

-- ── b5-nas ────────────────────────────────────────────────────────────────────

common("b5-nas")
docker("b5-nas")
docker_prune("b5-nas")

docker_compose("nextcloud", {
  host       = "b5-nas",
  dir        = "/opt/nextcloud",
  depends_on = { "docker-install-b5-nas" },
  env = {
    MYSQL_ROOT_PASSWORD      = nc_mysql_root_pw,
    MYSQL_PASSWORD           = nc_mysql_pw,
    NEXTCLOUD_ADMIN_PASSWORD = nc_admin_pw,
    REDIS_PASSWORD           = nc_redis_pw,
  },
  compose = [[
services:
  app:
    image: nextcloud:33.0.0-fpm-alpine
    restart: unless-stopped
    volumes:
      - ./data:/var/www/html
    environment:
      - MYSQL_HOST=db
      - MYSQL_DATABASE=nextcloud
      - MYSQL_USER=nextcloud
      - MYSQL_PASSWORD=${MYSQL_PASSWORD}
      - REDIS_HOST=redis
      - REDIS_HOST_PASSWORD=${REDIS_PASSWORD}
      - NEXTCLOUD_ADMIN_USER=admin
      - NEXTCLOUD_ADMIN_PASSWORD=${NEXTCLOUD_ADMIN_PASSWORD}
      - NEXTCLOUD_TRUSTED_DOMAINS=cloud.ju60.de
    depends_on:
      - db
      - redis

  db:
    image: mariadb:11.5
    restart: unless-stopped
    volumes:
      - ./db:/var/lib/mysql
    environment:
      - MYSQL_ROOT_PASSWORD=${MYSQL_ROOT_PASSWORD}
      - MYSQL_DATABASE=nextcloud
      - MYSQL_USER=nextcloud
      - MYSQL_PASSWORD=${MYSQL_PASSWORD}

  redis:
    image: redis:9.0.3-alpine
    restart: unless-stopped
    command: redis-server --requirepass ${REDIS_PASSWORD}
    volumes:
      - ./redis:/data

  nginx:
    image: nginx:1.28.2-alpine-slim
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./data:/var/www/html:ro
    depends_on:
      - app

  backup:
    image: tiredofit/db-backup:1.14.2
    restart: unless-stopped
    volumes:
      - ./backup:/backup
    environment:
      - DB_TYPE=mariadb
      - DB_HOST=db
      - DB_NAME=nextcloud
      - DB_USER=nextcloud
      - DB_PASS=${MYSQL_PASSWORD}
]],
})

docker_compose("immich", {
  host       = "b5-nas",
  dir        = "/opt/immich",
  depends_on = { "docker-install-b5-nas" },
  env = {
    IMMICH_VERSION  = "v2.6.3",
    DB_PASSWORD     = immich_db_pw,
    UPLOAD_LOCATION = "/data/immich",
  },
  compose = [[
services:
  immich-server:
    image: ghcr.io/immich-app/immich-server:${IMMICH_VERSION}
    restart: unless-stopped
    ports:
      - "2283:2283"
    volumes:
      - ${UPLOAD_LOCATION}:/usr/src/app/upload
      - /etc/localtime:/etc/localtime:ro
    environment:
      - DB_HOSTNAME=database
      - DB_USERNAME=postgres
      - DB_DATABASE_NAME=immich
      - DB_PASSWORD=${DB_PASSWORD}
      - REDIS_HOSTNAME=redis
    depends_on:
      - redis
      - database

  immich-machine-learning:
    image: ghcr.io/immich-app/immich-machine-learning:${IMMICH_VERSION}
    restart: unless-stopped
    volumes:
      - model-cache:/cache

  redis:
    image: redis:alpine
    restart: unless-stopped

  database:
    image: tensorchord/pgvecto-rs:pg14-v0.2.0
    restart: unless-stopped
    volumes:
      - ./db:/var/lib/postgresql/data
    environment:
      - POSTGRES_PASSWORD=${DB_PASSWORD}
      - POSTGRES_USER=postgres
      - POSTGRES_DB=immich

volumes:
  model-cache:
]],
})

docker_compose("home-assistant", {
  host       = "b5-nas",
  dir        = "/opt/home-assistant",
  depends_on = { "docker-install-b5-nas" },
  compose = [[
services:
  home-assistant:
    image: ghcr.io/home-assistant/home-assistant:2024.6.3
    restart: unless-stopped
    network_mode: host
    privileged: true
    volumes:
      - ./config:/config
      - /etc/localtime:/etc/localtime:ro

  matter-server:
    image: ghcr.io/home-assistant-libs/python-matter-server:stable
    restart: unless-stopped
    network_mode: host
    security_opt:
      - apparmor:unconfined
    volumes:
      - ./matter-data:/data
]],
})

docker_compose("papra", {
  host       = "b5-nas",
  dir        = "/opt/papra",
  depends_on = { "docker-install-b5-nas" },
  env = {
    AUTH_SECRET = papra_secret,
    DATA_DIR    = "/data/papra",
  },
  compose = [[
services:
  papra:
    image: ghcr.io/papra-hq/papra:26.3.0-rootless
    restart: unless-stopped
    ports:
      - "1234:1234"
    volumes:
      - ${DATA_DIR}:/data
    environment:
      - AUTH_SECRET=${AUTH_SECRET}
      - BASE_URL=https://files.ju60.de
      - DOCUMENTS_STORAGE_PATH=/data/documents
]],
})

-- ── hetzner04 ─────────────────────────────────────────────────────────────────

common("hetzner04")
docker("hetzner04")
docker_prune("hetzner04")

docker_compose("gitea", {
  host       = "hetzner04",
  dir        = "/opt/gitea",
  depends_on = { "docker-install-hetzner04" },
  env = {
    GITEA_VERSION = "1.25.5",
  },
  compose = [[
services:
  gitea:
    image: gitea/gitea:${GITEA_VERSION}
    restart: unless-stopped
    ports:
      - "3000:3000"
      - "222:22"
    volumes:
      - ./data:/data
      - /etc/localtime:/etc/localtime:ro
    environment:
      - USER_UID=1000
      - USER_GID=1000
      - GITEA__server__DOMAIN=git.ju60.de
      - GITEA__server__ROOT_URL=https://git.ju60.de
]],
})

docker_compose("memos", {
  host       = "hetzner04",
  dir        = "/opt/memos",
  depends_on = { "docker-install-hetzner04" },
  env = {
    MEMOS_VERSION = "0.26.2",
  },
  compose = [[
services:
  memos:
    image: neosmemo/memos:${MEMOS_VERSION}
    restart: unless-stopped
    ports:
      - "5230:5230"
    volumes:
      - ./data:/var/opt/memos
]],
})

docker_compose("vaultwarden", {
  host       = "hetzner04",
  dir        = "/opt/vaultwarden",
  depends_on = { "docker-install-hetzner04" },
  env = {
    VAULTWARDEN_VERSION = "1.35.4",
  },
  compose = [[
services:
  vaultwarden:
    image: vaultwarden/server:${VAULTWARDEN_VERSION}
    restart: unless-stopped
    ports:
      - "8080:80"
    volumes:
      - ./data:/data
    environment:
      - WEBSOCKET_ENABLED=true
      - SIGNUPS_ALLOWED=false
]],
})

docker_compose("uptime-kuma", {
  host       = "hetzner04",
  dir        = "/opt/uptime",
  depends_on = { "docker-install-hetzner04" },
  env = {
    UPTIME_KUMA_VERSION = "2.1.3-slim-rootless",
  },
  compose = [[
services:
  uptime-kuma:
    image: louislam/uptime-kuma:${UPTIME_KUMA_VERSION}
    restart: unless-stopped
    ports:
      - "3001:3001"
    volumes:
      - ./data:/app/data
]],
})
