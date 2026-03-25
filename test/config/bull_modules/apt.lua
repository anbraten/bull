-- apt(id, opts)
-- Ensures packages are installed via apt-get on a Debian/Ubuntu host.
--
-- opts:
--   host      (required) host name
--   packages  list of package names to install (alias: package for single)
--   update    bool — run apt-get update before installing (default false)
function apt(id, opts)
  assert(type(id) == "string", "apt: id must be a string")
  assert(type(opts) == "table", "apt: opts must be a table")
  local h = opts.host
  assert(h and h ~= "", "apt " .. id .. ": host is required")

  local pkgs
  if type(opts.packages) == "table" then
    pkgs = opts.packages
  elseif type(opts.package) == "string" then
    pkgs = { opts.package }
  else
    error("apt " .. id .. ": packages (list) or package (string) is required")
  end
  assert(#pkgs > 0, "apt " .. id .. ": packages list is empty")

  -- Check which packages from the list are currently installed.
  -- dpkg-query exits non-zero when any package is missing, so we suppress the
  -- exit code and parse stdout for what's installed.
  local function missing_packages()
    local list = table.concat(pkgs, " ")
    local out, _, err = ssh_exec(h, "dpkg-query -W -f '${Package}\\n' " .. list .. " 2>/dev/null; true")
    if err ~= "" then error("cannot reach host '" .. h .. "': " .. err) end

    local installed = {}
    for pkg in (out .. "\n"):gmatch("([^\n]+)\n") do
      if pkg ~= "" then installed[pkg] = true end
    end

    local missing = {}
    for _, pkg in ipairs(pkgs) do
      if not installed[pkg] then
        table.insert(missing, pkg)
      end
    end
    return missing
  end

  resource(id, {
    type = "apt",
    depends_on = opts.depends_on,

    plan = function()
      local missing = missing_packages()
      if #missing == 0 then
        return { change = "no-op" }
      end
      local diffs = {}
      for _, pkg in ipairs(missing) do
        table.insert(diffs, { field = "install", after = pkg })
      end
      return { change = "create", diffs = diffs }
    end,

    apply = function()
      if opts.update then
        local _, code, err = ssh_exec(h, "DEBIAN_FRONTEND=noninteractive apt-get update -qq 2>&1")
        if err ~= "" then error(err) end
        if code ~= 0 then error("apt-get update failed") end
      end

      -- Only install what's actually missing to keep output clean
      local missing = missing_packages()
      if #missing == 0 then return end

      local cmd = "DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "
        .. table.concat(missing, " ") .. " 2>&1"
      local out, code, err = ssh_exec(h, cmd)
      if err ~= "" then error(err) end
      if code ~= 0 then error("apt-get install failed:\n" .. out) end
    end,
  })
end
