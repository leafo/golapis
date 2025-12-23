

-- Test script to verify arg table behavior matches LuaJIT
-- Run with: golapis examples/test_args.lua foo bar baz
for i = -1, #arg do
    print(string.format("arg[%d] = %q", i, arg[i] or "nil"))
end

local varargs = {...}

for key, value in pairs(varargs) do
  print(string.format("...[%q] = %q", key, value))
end

io.flush()
