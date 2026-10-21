-- bootstrap.lua
-- Called during worker initialization with the golapis table
local golapis = ...

-- Complete coroutine module with wrap function
-- (wrap can't be implemented in C/Go since it returns a closure)
do
  local co_create = coroutine.create
  local co_resume = coroutine.resume

  local function pack(...)
    return { n = select("#", ...), ... }
  end

  function coroutine.wrap(func)
    local co = co_create(func)
    return function(...)
      local results = pack(co_resume(co, ...))
      if not results[1] then
        error(results[2], 2)
      end
      return unpack(results, 2, results.n)
    end
  end
end

-- Libraries written for OpenResty load the semaphore via require
package.preload["ngx.semaphore"] = function()
  return golapis.semaphore
end

-- ngx.re: match/find/gmatch/sub/gsub over the Go regex primitives. The
-- primitives return byte offsets; everything nginx-shaped (capture tables,
-- ctx.pos, nth, iterators, replacement templates) is built here.
do
  local re = golapis.re
  local exec, exec_all = re._exec, re._exec_all
  local sub, find = string.sub, string.find
  local type, tostring, pairs, ipairs, error = type, tostring, pairs, ipairs, error
  local tconcat = table.concat

  -- A usage error (bad argument, unknown flag) comes back as (nil, msg,
  -- true); raise it at the caller of the ngx.re function: level 3 from
  -- here (this helper, the ngx.re function, its caller), one more for
  -- sub/gsub which go through sub_helper. The frames in between must not
  -- be tail calls, which would drop them and shift the level.
  local function usage_error(msg, level)
    error(msg, level or 3)
  end

  -- Fill res with the captures of the match at offs[base+1 ..]: [0] is the
  -- whole match, unset groups are false, named groups are also keyed by name
  local function collect(subj, offs, base, ngroups, names, res)
    res = res or {}
    for i = 0, ngroups do
      local s = offs[base + 2 * i + 1]
      if s >= 0 then
        res[i] = sub(subj, s + 1, offs[base + 2 * i + 2])
      else
        res[i] = false
      end
    end
    if names then
      for i, name in pairs(names) do
        res[name] = res[i]
      end
    end
    return res
  end

  local function start_pos(ctx)
    if not ctx then return 0 end
    local pos = ctx.pos
    if not pos or pos <= 0 then return 0 end
    return pos - 1
  end

  -- Like lua-resty-core, the subject goes through tostring(): numbers
  -- match as their text and so does anything else
  function re.match(subj, regex, opts, ctx, res)
    subj = tostring(subj)
    local offs, ngroups, names = exec(subj, regex, opts, start_pos(ctx), "match")
    if not offs then
      if offs == nil then
        if names == true then usage_error(ngroups) end
        return nil, ngroups -- compile error
      end
      return nil
    end
    if ctx then ctx.pos = offs[2] + 1 end
    return collect(subj, offs, 0, ngroups, names, res)
  end

  function re.find(subj, regex, opts, ctx, nth)
    subj = tostring(subj)
    local offs, ngroups, names = exec(subj, regex, opts, start_pos(ctx), "find")
    if not offs then
      if offs == nil then
        if names == true then usage_error(ngroups) end
        return nil, nil, ngroups
      end
      return nil
    end
    if not nth or nth < 0 then nth = 0 end
    if nth > ngroups then return nil, nil, "nth out of bound" end
    if ctx then ctx.pos = offs[2] + 1 end
    local from = offs[2 * nth + 1]
    if from < 0 then return nil, nil end
    return from + 1, offs[2 * nth + 2]
  end

  -- The iterator searches lazily from the end of the previous match, so a
  -- loop that breaks early only pays for what it consumed
  function re.gmatch(subj, regex, opts)
    subj = tostring(subj)
    -- compile errors surface here rather than on the first iteration
    local offs, ngroups, names = exec(subj, regex, opts, 0, "gmatch")
    if offs == nil then
      if names == true then usage_error(ngroups) end
      return nil, ngroups
    end
    local pos, len, done = 0, #subj, offs == false
    return function()
      if done then return nil end
      if pos > 0 then
        offs = exec(subj, regex, opts, pos, "gmatch")
        if not offs then
          done = true
          return nil
        end
      end
      local s, e = offs[1], offs[2]
      pos = e
      if e == s then
        -- empty match: step forward so the next search makes progress
        pos = e + 1
        if pos > len then done = true end
      end
      return collect(subj, offs, 0, ngroups, names)
    end
  end

  -- Replacement templates: $0 $1 ${1} $name ${name} and $$ for a literal $.
  -- Compiled to a list of parts: a string is literal text, a number a group
  -- index, a table a named group {name}.
  local template_cache, template_cache_n = {}, 0

  local function template_ref(name)
    if find(name, "^%d+$") then return tonumber(name) end
    return { name }
  end

  local function compile_template(tpl)
    local parts = template_cache[tpl]
    if parts then return parts end
    parts = {}
    local i, len, lit = 1, #tpl, {}
    local function flush()
      if #lit > 0 then
        parts[#parts + 1] = tconcat(lit)
        lit = {}
      end
    end
    while i <= len do
      local s = find(tpl, "$", i, true)
      if not s then
        lit[#lit + 1] = sub(tpl, i)
        break
      end
      lit[#lit + 1] = sub(tpl, i, s - 1)
      local c = sub(tpl, s + 1, s + 1)
      if c == "$" then
        lit[#lit + 1] = "$"
        i = s + 2
      elseif c == "{" then
        local close = find(tpl, "}", s + 2, true)
        local name = close and sub(tpl, s + 2, close - 1)
        if not name or not find(name, "^[%w_]+$") then
          return nil, 'invalid capturing variable name found in "' .. tpl .. '"'
        end
        flush()
        parts[#parts + 1] = template_ref(name)
        i = close + 1
      else
        local name = tpl:match("^%d+", s + 1) or tpl:match("^[%a_][%w_]*", s + 1)
        if not name then
          return nil, 'invalid capturing variable name found in "' .. tpl .. '"'
        end
        flush()
        parts[#parts + 1] = template_ref(name)
        i = s + 1 + #name
      end
    end
    flush()
    if template_cache_n >= 256 then
      template_cache, template_cache_n = {}, 0
    end
    template_cache[tpl] = parts
    template_cache_n = template_cache_n + 1
    return parts
  end

  -- Every reference in the template must name a group of this regex
  local function check_template(parts, ngroups, names)
    local byname
    for _, p in ipairs(parts) do
      if type(p) == "number" then
        if p > ngroups then
          return "the capture $" .. p .. " is out of range (" .. ngroups .. " capturing groups)"
        end
      elseif type(p) == "table" then
        if not byname then
          byname = {}
          if names then
            for _, name in pairs(names) do byname[name] = true end
          end
        end
        if not byname[p[1]] then
          return 'unknown named capture "' .. p[1] .. '" in the replacement template'
        end
      end
    end
    return nil
  end

  local function expand(parts, caps)
    local out = {}
    for i = 1, #parts do
      local p = parts[i]
      local t = type(p)
      if t == "number" then
        out[i] = caps[p] or ""
      elseif t == "table" then
        out[i] = caps[p[1]] or ""
      else
        out[i] = p
      end
    end
    return tconcat(out)
  end

  local function sub_helper(subj, regex, replace, opts, global)
    subj = tostring(subj)
    local fname = global and "gsub" or "sub"
    local offs, n, ngroups, names
    if global then
      offs, n, ngroups, names = exec_all(subj, regex, opts, fname)
      if not offs then
        if ngroups == true then usage_error(n, 4) end
        return nil, nil, n
      end
    else
      offs, ngroups, names = exec(subj, regex, opts, 0, fname)
      if offs == nil then
        if names == true then usage_error(ngroups, 4) end
        return nil, nil, ngroups
      end
      n = offs and 1 or 0
    end

    -- The template is checked even when nothing matches, like nginx
    local is_func = type(replace) == "function"
    local parts
    if not is_func then
      local err
      parts, err = compile_template(tostring(replace))
      if not parts then return nil, nil, err end
      err = check_template(parts, ngroups, names)
      if err then return nil, nil, err end
    end
    if n == 0 then return subj, 0 end

    local stride = 2 * (ngroups + 1)
    local out, last = {}, 0
    for m = 0, n - 1 do
      local base = m * stride
      local s, e = offs[base + 1], offs[base + 2]
      out[#out + 1] = sub(subj, last + 1, s)
      local caps = collect(subj, offs, base, ngroups, names)
      if is_func then
        -- tostring like lua-resty-core: a nil return becomes "nil"
        out[#out + 1] = tostring(replace(caps))
      else
        out[#out + 1] = expand(parts, caps)
      end
      last = e
    end
    out[#out + 1] = sub(subj, last + 1)
    return tconcat(out), n
  end

  function re.sub(subj, regex, replace, opts)
    local s, n, err = sub_helper(subj, regex, replace, opts, false)
    if err then return nil, nil, err end
    return s, n
  end

  function re.gsub(subj, regex, replace, opts)
    local s, n, err = sub_helper(subj, regex, replace, opts, true)
    if err then return nil, nil, err end
    return s, n
  end

  package.preload["ngx.re"] = function()
    return re
  end
end

-- Load HTTP module wrapper (provides LuaSocket-style interface)
do
  local http_mod = assert(loadstring(golapis._http_src, "@http.lua"))()
  golapis.http.request = http_mod.request
  golapis._http_src = nil  -- Clean up after loading
end
