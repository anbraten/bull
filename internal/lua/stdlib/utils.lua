-- _validate_opts(fn_name, id, opts, known_keys)
-- Raises an error if opts contains any key not in known_keys.
function _validate_opts(fn_name, id, opts, known_keys)
  local known = {}
  for _, k in ipairs(known_keys) do known[k] = true end
  local unknown_list = {}
  for k in pairs(opts) do
    if not known[k] then
      local hint = k == "depends" and " (did you mean 'depends_on'?)" or ""
      table.insert(unknown_list, "'" .. k .. "'" .. hint)
    end
  end
  if #unknown_list > 0 then
    table.sort(unknown_list)
    error(fn_name .. " " .. id .. ": unknown option(s): " .. table.concat(unknown_list, ", "))
  end
end

-- _remote_file_info(host, path) → { hash = <sha256 hex>, mode = "644" } or nil
-- Returns nil if the path is not a regular file. Raises on SSH failure or if
-- the file can't be read (e.g. permission denied).
function _remote_file_info(h, path)
  local p = q(path)
  local out, code, err = ssh_exec(h,
    "if [ -f " .. p .. " ]; then sha256sum " .. p .. " && stat -c %a " .. p .. "; else echo missing; fi")
  if err ~= "" then error("cannot reach host '" .. h .. "': " .. err) end
  if code ~= 0 then error("inspect " .. path .. ": " .. out) end
  if out == "missing" then return nil end
  local hash, mode = out:match("^(%x+)%s+[^\n]*\n(%d+)$")
  if not hash then error("inspect " .. path .. ": unexpected output: " .. out) end
  return { hash = hash, mode = mode }
end
