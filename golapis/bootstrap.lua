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
