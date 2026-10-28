-- file(id, opts)
-- Ensures a file exists on a remote host with specific content.
--
-- opts:
--   host        (required)
--   path        remote path (alias: remote_path)
--   content     inline file content
--   local_path  read content from local file (alias: src)
--   mode        octal permission string, e.g. "0644" (default "0644")
function file(id, opts)
  assert(type(id) == "string", "file: id must be a string")
  assert(type(opts) == "table", "file: opts must be a table")
  _validate_opts("file", id, opts, { "host", "path", "remote_path", "content", "local_path", "src", "mode", "depends_on" })
  local h = opts.host
  assert(h and h ~= "", "file " .. id .. ": host is required")
  local remote = opts.path or opts.remote_path
  assert(remote and remote ~= "", "file " .. id .. ": path is required")

  local function get_content()
    if opts.content then return opts.content end
    local src = opts.local_path or opts.src
    if src then return read_file(src) end
    error("file " .. id .. ": content or local_path is required")
  end

  local mode = tonumber(opts.mode or "0644", 8) or 0x1A4

  resource(id, {
    type = "file",
    depends_on = opts.depends_on,

    plan = function()
      local desired_hash = sha256(get_content())
      local desired_mode = string.format("%o", mode)

      local current = _remote_file_info(h, remote)
      if not current then
        return {
          change = "create",
          diffs = {
            { field = "path",         after = remote },
            { field = "content_hash", after = desired_hash:sub(1, 12) },
            { field = "mode",         after = desired_mode },
          },
        }
      end

      local diffs = {}
      if current.hash ~= desired_hash then
        table.insert(diffs, { field = "content_hash", before = current.hash:sub(1, 12), after = desired_hash:sub(1, 12) })
      end
      if current.mode ~= desired_mode then
        table.insert(diffs, { field = "mode", before = current.mode, after = desired_mode })
      end
      if #diffs == 0 then
        return { change = "no-op" }
      end
      return { change = "update", diffs = diffs }
    end,

    apply = function()
      local content = get_content()
      local err = ssh_upload(h, remote, content, mode)
      if err ~= "" then error("upload failed: " .. err) end
    end,
  })
end
