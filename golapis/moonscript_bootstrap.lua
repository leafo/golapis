-- moonscript_bootstrap.lua
-- Receives entrypoint filename, installs moonloader, compiles and returns the chunk

local entrypoint = ...

-- Load moonscript (will error if not installed)
local moonscript = require("moonscript")
local moon_base = require("moonscript.base")

-- The moonscript compiler internally uses coroutine.create/resume. Golapis
-- replaces those globally with thread-context-aware variants that yield to the
-- scheduler. Two problems follow:
--   1. loadMoonFile runs this bootstrap on the main state with no thread
--      context, so the hooked coroutine fns error with "no thread context".
--   2. The moon loader is registered in package.loaders, so any later
--      require("foo") that finds foo.moon triggers compilation from inside the
--      C `require` frame -- and the hooked coroutine.resume always yields to
--      the scheduler, producing "attempt to yield across C-call boundary".
-- Both cases need the underscore-prefixed originals (preserved by
-- setup_coroutine_module in bindings.go) for the duration of the compile.
local function with_native_coroutine(fn, ...)
  local saved = {
    create = coroutine.create,
    resume = coroutine.resume,
    yield = coroutine.yield,
    status = coroutine.status,
    running = coroutine.running,
    wrap = coroutine.wrap,
  }
  if coroutine._create then coroutine.create = coroutine._create end
  if coroutine._resume then coroutine.resume = coroutine._resume end
  if coroutine._yield then coroutine.yield = coroutine._yield end
  if coroutine._status then coroutine.status = coroutine._status end
  if coroutine._running then coroutine.running = coroutine._running end
  if coroutine._wrap then coroutine.wrap = coroutine._wrap end

  local ok, a, b, c = pcall(fn, ...)

  coroutine.create = saved.create
  coroutine.resume = saved.resume
  coroutine.yield = saved.yield
  coroutine.status = saved.status
  coroutine.running = saved.running
  coroutine.wrap = saved.wrap

  if not ok then error(a, 0) end
  return a, b, c
end

-- Install the moon loader, then wrap it so it uses native coroutines for
-- per-require compilation.
moon_base.insert_loader()
local loaders = package.loaders or package.searchers
for i, loader in ipairs(loaders) do
  if loader == moon_base.moon_loader then
    loaders[i] = function(name)
      return with_native_coroutine(moon_base.moon_loader, name)
    end
    break
  end
end

local fn, err = with_native_coroutine(moonscript.loadfile, entrypoint)

-- moonscript.loadfile returns (nil, err) on failure; surface that as an error
-- so callers see the real message instead of silently storing nil as the
-- entrypoint and getting a bare "attempt to call a nil value" when invoked.
if not fn then
  error(err or ("failed to load moonscript file: " .. tostring(entrypoint)), 0)
end
return fn
