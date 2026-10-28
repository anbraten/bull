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
