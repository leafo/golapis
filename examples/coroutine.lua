-- this file test that user created coroutines can access the parent LuaThread
-- and are scheduled by the event loop

local co = coroutine.create(function()
  print("before sleep")
  print(golapis.sleep(1))
  print("after sleep")
end)

print("starting..")
print(coroutine.resume(co))

