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
        golapis.say("[callback] Timer cancelled early, msg:", msg)
        return
    end
    golapis.say("[callback] Timer fired! msg:", msg, "count:", count)
end

golapis.say("Scheduling timers...")

-- Schedule a timer with arguments
local ok, err = golapis.timer.at(0.5, my_callback, "hello", 42)
if not ok then
    golapis.say("Failed to create timer:", err)
else
    golapis.say("Timer 1 scheduled (0.5s delay)")
end

-- Schedule a quick timer with no extra args
golapis.timer.at(0.2, function(premature)
    if not premature then
        golapis.say("[callback] Quick timer fired!")
    end
end)
golapis.say("Timer 2 scheduled (0.2s delay)")

-- Timer callback can use async operations
golapis.timer.at(0.3, function(premature)
    if premature then return end
    golapis.say("[callback] Async timer started, sleeping 0.1s...")
    golapis.sleep(0.1)
    golapis.say("[callback] Async timer done!")
end)
golapis.say("Timer 3 scheduled (0.3s delay, with async sleep)")

golapis.say("Main thread done. Wait() will block until all timers complete.")
