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
  _validate_opts("docker_compose", id, opts, { "host", "dir", "directory", "compose_file", "file", "compose", "env_file", "env", "depends_on" })
  local h = opts.host
  assert(h and h ~= "", "docker_compose " .. id .. ": host is required")
  local dir = opts.dir or opts.directory
  assert(dir and dir ~= "", "docker_compose " .. id .. ": dir is required")
  assert(opts.compose or opts.compose_file or opts.file,
    "docker_compose " .. id .. ": compose or compose_file is required")

  local remote_compose = dir .. "/docker-compose.yml"
  local remote_env = dir .. "/.env"

  local function get_compose()
    if opts.compose then return opts.compose end
    return bull.read_file(opts.compose_file or opts.file)
  end

  local function get_env_content()
    if opts.env_file then return bull.read_file(opts.env_file) end
    if opts.env then
      local lines = {}
      for k, v in pairs(opts.env) do
        table.insert(lines, k .. "=" .. tostring(v))
      end
      table.sort(lines)
      return table.concat(lines, "\n") .. "\n"
    end
    return nil
  end

  bull.resource(id, {
    type = "docker_compose",
    depends_on = opts.depends_on,

    plan = function()
      local compose = get_compose()
      local env_content = get_env_content()

      local current = _remote_file_info(h, remote_compose)
      if not current then
        local diffs = {
          { field = "host", after = h },
          { field = "dir",  after = dir },
          _content_diff("compose", h, remote_compose, nil, compose),
        }
        if env_content then
          table.insert(diffs, _content_diff("env", h, remote_env, nil, env_content))
        end
        return { change = "create", diffs = diffs }
      end

      local diffs = {}
      local compose_diff = _content_diff("compose", h, remote_compose, current, compose)
      if compose_diff then table.insert(diffs, compose_diff) end
      -- Only compare .env when bull manages it.
      if env_content then
        local current_env = _remote_file_info(h, remote_env)
        local env_diff = _content_diff("env", h, remote_env, current_env, env_content)
        if env_diff then table.insert(diffs, env_diff) end
      end

      if #diffs == 0 then
        return { change = "no-op" }
      end
      return { change = "update", diffs = diffs }
    end,

    apply = function()
      local desired = get_compose()

      -- Ensure directory exists
      local _, _, err = bull.ssh.exec(h, "mkdir -p " .. bull.q(dir))
      if err ~= "" then error("mkdir failed: " .. err) end

      -- Upload compose file
      local upload_err = bull.ssh.upload(h, remote_compose, desired)
      if upload_err ~= "" then error("upload compose: " .. upload_err) end

      -- Upload .env if provided
      local env_content = get_env_content()
      if env_content then
        upload_err = bull.ssh.upload(h, remote_env, env_content, 0x180) -- 0600
        if upload_err ~= "" then error("upload .env: " .. upload_err) end
      end

      -- Bring stack up
      local out, code, exec_err = bull.ssh.exec(h,
        "docker compose -f " .. bull.q(remote_compose) .. " up -d --remove-orphans --pull always 2>&1")
      if exec_err ~= "" then error(exec_err) end
      if code ~= 0 then error("docker compose up failed:\n" .. out) end
    end,
  })
end
