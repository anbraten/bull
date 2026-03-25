-- shell(id, opts)
-- Runs a command on a remote host via SSH.
--
-- opts:
--   host     (required) host name declared with host()
--   command  (required) shell command to run (aliases: cmd, run)
--   creates  remote path — skip command if this path already exists
--   unless   shell expression — skip command if this exits 0
function shell(id, opts)
  assert(type(id) == "string", "shell: id must be a string")
  assert(type(opts) == "table", "shell: opts must be a table")
  local h = opts.host
  assert(h and h ~= "", "shell " .. id .. ": host is required")
  local cmd = opts.command or opts.cmd or opts.run
  assert(cmd and cmd ~= "", "shell " .. id .. ": command is required")

  resource(id, {
    type = "shell",
    depends_on = opts.depends_on,

    plan = function()
      -- creates guard: skip if remote path exists
      if opts.creates then
        local _, code, err = ssh_exec(h, "test -e " .. q(opts.creates))
        if err ~= "" then
          error("cannot reach host '" .. h .. "': " .. err)
        end
        if code == 0 then
          return { change = "no-op" }
        end
        return {
          change = "create",
          diffs = { { field = "creates", after = opts.creates } },
        }
      end

      -- unless guard: skip if check command exits 0
      if opts.unless then
        local _, code, err = ssh_exec(h, opts.unless)
        if err ~= "" then
          error("cannot reach host '" .. h .. "': " .. err)
        end
        if code == 0 then
          return { change = "no-op" }
        end
      end

      -- no guard → always runs
      local display = cmd:sub(1, 60) .. (cmd:len() > 60 and "…" or "")
      return {
        change = "create",
        diffs = { { field = "command", after = display } },
      }
    end,

    apply = function()
      local out, code, err = ssh_exec(h, cmd)
      if err ~= "" then error(err) end
      if code ~= 0 then
        error("command exited " .. code .. ": " .. out)
      end
    end,
  })
end
