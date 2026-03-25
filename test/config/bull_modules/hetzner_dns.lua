-- hetzner_dns(id, opts)
-- Manages a DNS record via the Hetzner DNS API.
-- Requires: curl available locally, HETZNER_DNS_TOKEN env var (or token= in opts)
--
-- opts:
--   zone   (required) zone name, e.g. "example.com"
--   name   (required) record name, e.g. "photos" → photos.example.com
--   value  (required) record value, e.g. "1.2.3.4"
--   type   record type (default "A")
--   ttl    TTL in seconds (default 300)
--   token  API token (default: $HETZNER_DNS_TOKEN)
function hetzner_dns(id, opts)
  assert(type(id) == "string", "hetzner_dns: id must be a string")
  assert(type(opts) == "table", "hetzner_dns: opts must be a table")
  assert(opts.zone  and opts.zone  ~= "", "hetzner_dns " .. id .. ": zone is required")
  assert(opts.name  and opts.name  ~= "", "hetzner_dns " .. id .. ": name is required")
  assert(opts.value and opts.value ~= "", "hetzner_dns " .. id .. ": value is required")

  local rec_type = opts.type  or "A"
  local ttl      = opts.ttl   or 300

  -- Build a curl command for the Hetzner DNS API
  local function api(method, path, body)
    local token = opts.token or env("HETZNER_DNS_TOKEN", "")
    if token == "" then
      error("hetzner_dns " .. id .. ": token required (set HETZNER_DNS_TOKEN or token= in opts)")
    end

    local cmd = "curl -sS -w '\\n%{http_code}' -X " .. method
      .. " -H " .. q("Auth-API-Token: " .. token)

    if body then
      local encoded, enc_err = json_encode(body)
      if enc_err then error("json_encode: " .. enc_err) end
      cmd = cmd .. " -H 'Content-Type: application/json' -d " .. q(encoded)
    end

    cmd = cmd .. " 'https://dns.hetzner.com/api/v1" .. path .. "'"

    local out, code, exec_err = local_exec(cmd)
    if exec_err ~= "" then error("curl failed: " .. exec_err) end
    if code ~= 0 then error("curl exited " .. code) end

    -- Last line is the HTTP status, rest is the body
    local body_lines, status_line = {}, ""
    for line in (out .. "\n"):gmatch("([^\n]*)\n") do
      if line ~= "" then
        status_line = line
        table.insert(body_lines, line)
      end
    end
    local status = tonumber(status_line)
    table.remove(body_lines)
    local resp_body = table.concat(body_lines, "\n")

    local parsed, parse_err = json_decode(resp_body)
    if parse_err then error("json_decode: " .. parse_err .. "\nBody: " .. resp_body) end

    return parsed, status
  end

  local function get_zone_id()
    local resp, status = api("GET", "/zones?name=" .. opts.zone, nil)
    if status ~= 200 then
      error("get zone failed (HTTP " .. status .. ") for zone: " .. opts.zone)
    end
    for _, z in ipairs(resp.zones or {}) do
      if z.name == opts.zone then return z.id end
    end
    error("zone not found: " .. opts.zone)
  end

  local function find_record(zone_id)
    local resp, status = api("GET", "/records?zone_id=" .. zone_id, nil)
    if status ~= 200 then error("list records failed: HTTP " .. status) end
    for _, r in ipairs(resp.records or {}) do
      if r.name == opts.name and r.type == rec_type then
        return r
      end
    end
    return nil
  end

  resource(id, {
    type = "hetzner_dns",
    depends_on = opts.depends_on,

    plan = function()
      local zone_id = get_zone_id()
      local record  = find_record(zone_id)

      if not record then
        return {
          change = "create",
          diffs = {
            { field = "name",  after = opts.name .. "." .. opts.zone },
            { field = "type",  after = rec_type },
            { field = "value", after = opts.value },
            { field = "ttl",   after = tostring(ttl) },
          },
        }
      end

      local diffs = {}
      if record.value ~= opts.value then
        table.insert(diffs, { field = "value", before = record.value, after = opts.value })
      end
      if record.ttl ~= ttl then
        table.insert(diffs, { field = "ttl", before = tostring(record.ttl), after = tostring(ttl) })
      end

      if #diffs == 0 then return { change = "no-op" } end
      return { change = "update", diffs = diffs }
    end,

    apply = function()
      local zone_id = get_zone_id()
      local record  = find_record(zone_id)

      local payload = {
        zone_id = zone_id,
        type    = rec_type,
        name    = opts.name,
        value   = opts.value,
        ttl     = ttl,
      }

      if not record then
        local _, status = api("POST", "/records", payload)
        if status ~= 201 then error("create record failed: HTTP " .. status) end
      else
        local _, status = api("PUT", "/records/" .. record.id, payload)
        if status ~= 200 then error("update record failed: HTTP " .. status) end
      end
    end,
  })
end
