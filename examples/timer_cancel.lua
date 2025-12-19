-- Example: golapis.debug.cancel_timers() and golapis.debug.pending_timer_count()
--
-- Demonstrates premature timer cancellation using the debug function.
-- When cancel_timers() is called, all pending timers fire immediately
-- with premature=true.
--
-- This example shows:
-- - A 500ms timer that fires naturally (before we cancel)
-- - A 2s and 10s timer that get cancelled prematurely
-- - Using pending_timer_count() to track how many timers are waiting
--
-- Run with: ./bin/golapis examples/timer_cancel.lua

local function callback(premature, name)
    if premature then
        golapis.print("[" .. name .. "] Timer cancelled prematurely!")
    else
        golapis.print("[" .. name .. "] Timer fired normally")
    end
end

golapis.print("Pending timers: " .. golapis.debug.pending_timer_count())

golapis.print("Scheduling timers...")

golapis.timer.at(0.5, callback, "500ms")
golapis.timer.at(2, callback, "2s")
golapis.timer.at(10, callback, "10s")

golapis.print("Pending timers: " .. golapis.debug.pending_timer_count())
golapis.print("Sleeping for 1 second to let 500ms timer fire naturally...")

golapis.sleep(1)

golapis.print("Pending timers: " .. golapis.debug.pending_timer_count())
golapis.print("Cancelling remaining timers...")
golapis.debug.cancel_timers()
golapis.debug.cancel_timers()

golapis.print("Pending timers: " .. golapis.debug.pending_timer_count())
golapis.print("Done!")
