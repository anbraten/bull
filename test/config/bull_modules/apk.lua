-- apk(id, opts)
-- Ensures packages are installed via apk on an Alpine Linux host.
--
-- opts:
--   host       (required) host name
--   packages   list of package names (alias: package for single)
--   update     bool — run apk update before installing (default false)
--   repository extra repository URL to add before installing (optional)
function apk(id, opts)
  assert(type(id) == "string", "apk: id must be a string")
  assert(type(opts) == "table", "apk: opts must be a table")
  local h = opts.host
  assert(h and h ~= "", "apk " .. id .. ": host is required")

  local pkgs
  if type(opts.packages) == "table" then
    pkgs = opts.packages
  elseif type(opts.package) == "string" then
    pkgs = { opts.package }
  else
    error("apk " .. id .. ": packages (list) or package (string) is required")
  end
  assert(#pkgs > 0, "apk " .. id .. ": packages list is empty")

  -- apk info -e exits 0 only if ALL packages are installed.
  -- We check each individually so we can report which are missing.
  local function missing_packages()
    local check = "for p in " .. table.concat(pkgs, " ")
      .. "; do apk info -e \"$p\" > /dev/null 2>&1 && echo \"$p\"; done"
    local out, _, err = ssh_exec(h, check)
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
    type = "apk",
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
      -- Add extra repository if specified
      if opts.repository then
        local _, code, err = ssh_exec(h,
          "echo " .. q(opts.repository) .. " >> /etc/apk/repositories")
        if err ~= "" then error(err) end
        if code ~= 0 then error("failed to add repository") end
      end

      if opts.update then
        local _, code, err = ssh_exec(h, "apk update -q 2>&1")
        if err ~= "" then error(err) end
        if code ~= 0 then error("apk update failed") end
      end

      local missing = missing_packages()
      if #missing == 0 then return end

      local out, code, err = ssh_exec(h,
        "apk add -q " .. table.concat(missing, " ") .. " 2>&1")
      if err ~= "" then error(err) end
      if code ~= 0 then error("apk add failed:\n" .. out) end
    end,
  })
end
