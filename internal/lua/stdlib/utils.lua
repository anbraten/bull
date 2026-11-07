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

-- _remote_file_info(host, path) → { hash = <sha256 hex>, mode = "644", size = <bytes> } or nil
-- Returns nil if the path is not a regular file. Raises on SSH failure or if
-- the file can't be read (e.g. permission denied).
function _remote_file_info(h, path)
  local p = q(path)
  local out, code, err = ssh_exec(h,
    "if [ -f " .. p .. " ]; then sha256sum " .. p .. " && stat -c '%a %s' " .. p .. "; else echo missing; fi")
  if err ~= "" then error("cannot reach host '" .. h .. "': " .. err) end
  if code ~= 0 then error("inspect " .. path .. ": " .. out) end
  if out == "missing" then return nil end
  local hash, mode, size = out:match("^(%x+)%s+[^\n]*\n(%d+) (%d+)$")
  if not hash then error("inspect " .. path .. ": unexpected output: " .. out) end
  return { hash = hash, mode = mode, size = tonumber(size) }
end

-- Files larger than this are compared by hash only; their content is not
-- downloaded or shown in the plan.
_MAX_TEXT_DIFF = 512 * 1024

-- _content_diff(field, host, path, current, desired) → diff entry or nil
-- current is the result of _remote_file_info (nil when the file is missing).
-- Returns nil when the content matches. Otherwise returns a text diff
-- ({ field, before, after, text = true }), downloading the remote content
-- only when it differs. Falls back to a "<field>_hash" diff for large files.
function _content_diff(field, h, path, current, desired)
  local desired_hash = sha256(desired)
  if current and current.hash == desired_hash then return nil end

  if #desired > _MAX_TEXT_DIFF or (current and current.size > _MAX_TEXT_DIFF) then
    return {
      field = field .. "_hash",
      before = current and current.hash:sub(1, 12) or nil,
      after = desired_hash:sub(1, 12),
    }
  end

  local before = nil
  if current then
    local content, err = ssh_read(h, path)
    if err ~= "" then error("cannot read " .. path .. " on '" .. h .. "': " .. err) end
    before = content
  end
  return { field = field, before = before, after = desired, text = true }
end
