-- Example: golapis.timer.at(delay, callback, arg...)
--
-- Schedules a callback to execute after a delay.
-- The callback receives (premature, arg1, arg2, ...) where:
--   - premature: true if timer was cancelled early (e.g., during shutdown)
--   - arg1, arg2, ...: optional arguments passed to timer.at
--
-- Run with: ./bin/golapis examples/timer.lua

local function my_callback(premature, msg, count)
    if premature then
        golapis.print("[callback] Timer cancelled early, msg:", msg)
        return
    end
    golapis.print("[callback] Timer fired! msg:", msg, "count:", count)
end

golapis.print("Scheduling timers...")

-- Schedule a timer with arguments
local ok, err = golapis.timer.at(0.5, my_callback, "hello", 42)
if not ok then
    golapis.print("Failed to create timer:", err)
else
    golapis.print("Timer 1 scheduled (0.5s delay)")
end

-- Schedule a quick timer with no extra args
golapis.timer.at(0.2, function(premature)
    if not premature then
        golapis.print("[callback] Quick timer fired!")
    end
end)
golapis.print("Timer 2 scheduled (0.2s delay)")

-- Timer callback can use async operations
golapis.timer.at(0.3, function(premature)
    if premature then return end
    golapis.print("[callback] Async timer started, sleeping 0.1s...")
    golapis.sleep(0.1)
    golapis.print("[callback] Async timer done!")
end)
golapis.print("Timer 3 scheduled (0.3s delay, with async sleep)")

golapis.print("Main thread done. Wait() will block until all timers complete.")
