-- docker_compose(id, opts)
-- Manages a Docker Compose stack on a remote host.
--
-- opts:
--   host         (required) host name
--   dir          (required) remote directory for the stack
--   compose_file path to local compose file (alias: file)
--   compose      inline compose content
--   env_file     path to local .env file
--   env          table of KEY=VALUE pairs (written as .env)
function docker_compose(id, opts)
  assert(type(id) == "string", "docker_compose: id must be a string")
  assert(type(opts) == "table", "docker_compose: opts must be a table")
  local h = opts.host
  assert(h and h ~= "", "docker_compose " .. id .. ": host is required")
  local dir = opts.dir or opts.directory
  assert(dir and dir ~= "", "docker_compose " .. id .. ": dir is required")
  assert(opts.compose or opts.compose_file or opts.file,
    "docker_compose " .. id .. ": compose or compose_file is required")

  local remote_compose = dir .. "/docker-compose.yml"

  local function get_compose()
    if opts.compose then return opts.compose end
    return read_file(opts.compose_file or opts.file)
  end

  local function get_env_content()
    if opts.env_file then return read_file(opts.env_file) end
    if opts.env then
      local lines = {}
      for k, v in pairs(opts.env) do
        table.insert(lines, k .. "=" .. v)
      end
      table.sort(lines)
      return table.concat(lines, "\n") .. "\n"
    end
    return nil
  end

  resource(id, {
    type = "docker_compose",
    depends_on = opts.depends_on,

    plan = function()
      local desired = get_compose()
      local desired_hash = sha256(desired):sub(1, 12)

      -- Check if compose file exists on remote
      local _, code, err = ssh_exec(h, "test -f " .. q(remote_compose))
      if err ~= "" then
        error("cannot reach host '" .. h .. "': " .. err)
      end

      if code ~= 0 then
        return {
          change = "create",
          diffs = {
            { field = "host",         after = h },
            { field = "dir",          after = dir },
            { field = "compose_hash", after = desired_hash },
          },
        }
      end

      -- Compare hashes
      local current_hash, _, _ = ssh_exec(h, "sha256sum " .. q(remote_compose) .. " | cut -d' ' -f1")
      current_hash = current_hash:sub(1, 12)

      if current_hash == desired_hash then
        return { change = "no-op" }
      end

      return {
        change = "update",
        diffs = {
          { field = "compose_hash", before = current_hash, after = desired_hash },
        },
      }
    end,

    apply = function()
      local desired = get_compose()

      -- Ensure directory exists
      local _, _, err = ssh_exec(h, "mkdir -p " .. q(dir))
      if err ~= "" then error("mkdir failed: " .. err) end

      -- Upload compose file
      local upload_err = ssh_upload(h, remote_compose, desired)
      if upload_err ~= "" then error("upload compose: " .. upload_err) end

      -- Upload .env if provided
      local env_content = get_env_content()
      if env_content then
        upload_err = ssh_upload(h, dir .. "/.env", env_content, 0x180) -- 0600
        if upload_err ~= "" then error("upload .env: " .. upload_err) end
      end

      -- Bring stack up
      local out, code, exec_err = ssh_exec(h,
        "docker compose -f " .. q(remote_compose) .. " up -d --remove-orphans --pull always 2>&1")
      if exec_err ~= "" then error(exec_err) end
      if code ~= 0 then error("docker compose up failed:\n" .. out) end
    end,
  })
end
