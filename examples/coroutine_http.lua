-- this file is to verify that user created coroutines have access to request
-- context and are scheduled via the event loop

print("global ctx:", tostring(golapis.ctx))
print("global co:", tostring(coroutine.running()))
print("global method:", tostring(golapis.var.request_method))

-- try with pcall
pcall(function()
  print("pcall ctx:", tostring(golapis.ctx))
  print("pcall co:", tostring(coroutine.running()))
  print("pcall method:", tostring(golapis.var.request_method))
end)

-- coroutine based capture errors should not break any request context access
local function capture_errors(fn, ...)
  local co = coroutine.create(fn)
  local success, err = coroutine.resume(co, ...)
  if not success then
    error(debug.traceback(co, err))
  end
end

capture_errors(function()
  print("capture ctx:", tostring(golapis.ctx))
  print("capture co:", tostring(coroutine.running()))
  print("capture method:", tostring(golapis.var.request_method))
  golapis.sleep(0)
  print("capture slept:")
end)

