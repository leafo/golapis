package golapis

import "testing"

func TestTimerCancelExampleShort(t *testing.T) {
	code := `
		local normal_fired = 0
		local cancelled_fired = 0

		local function callback(premature, name)
			if premature then
				cancelled_fired = cancelled_fired + 1
				golapis.say("[" .. name .. "] Timer cancelled prematurely!")
			else
				normal_fired = normal_fired + 1
				golapis.say("[" .. name .. "] Timer fired normally")
			end
		end

		golapis.say("Pending timers: " .. golapis.debug.pending_timer_count())
		golapis.say("Scheduling timers...")

		golapis.timer.at(0.02, callback, "20ms")
		golapis.timer.at(0.05, callback, "50ms")
		golapis.timer.at(0.2, callback, "200ms")

		golapis.say("Pending timers: " .. golapis.debug.pending_timer_count())
		golapis.say("Sleeping for 0.1 second to let 20ms and 50ms timers fire naturally...")

		golapis.sleep(0.1)

		while normal_fired < 2 do
			golapis.sleep(0.01)
		end

		golapis.say("Pending timers: " .. golapis.debug.pending_timer_count())
		golapis.say("Cancelling remaining timers...")
		golapis.debug.cancel_timers()
		golapis.debug.cancel_timers()

		while cancelled_fired < 1 do
			golapis.sleep(0.01)
		end

		golapis.say("Pending timers: " .. golapis.debug.pending_timer_count())
		golapis.say("Done!")
	`

	expected := "" +
		"Pending timers: 0\n" +
		"Scheduling timers...\n" +
		"Pending timers: 3\n" +
		"Sleeping for 0.1 second to let 20ms and 50ms timers fire naturally...\n" +
		"[20ms] Timer fired normally\n" +
		"[50ms] Timer fired normally\n" +
		"Pending timers: 1\n" +
		"Cancelling remaining timers...\n" +
		"[200ms] Timer cancelled prematurely!\n" +
		"Pending timers: 0\n" +
		"Done!\n"

	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if output != expected {
		t.Errorf("output mismatch\ngot:  %q\nwant: %q", output, expected)
	}
}

func TestTimerExampleShort(t *testing.T) {
	code := `
		local function my_callback(premature, msg, count)
			if premature then
				golapis.say("[callback] Timer cancelled early, msg:", msg)
				return
			end
			golapis.say("[callback] Timer fired! msg:", msg, "count:", count)
		end

		golapis.say("Scheduling timers...")

		local ok, err = golapis.timer.at(0.05, my_callback, "hello", 42)
		if not ok then
			golapis.say("Failed to create timer:", err)
		else
			golapis.say("Timer 1 scheduled (0.05s delay)")
		end

		golapis.timer.at(0.02, function(premature)
			if not premature then
				golapis.say("[callback] Quick timer fired!")
			end
		end)
		golapis.say("Timer 2 scheduled (0.02s delay)")

		golapis.timer.at(0.03, function(premature)
			if premature then return end
			golapis.say("[callback] Async timer started, sleeping 0.01s...")
			golapis.sleep(0.01)
			golapis.say("[callback] Async timer done!")
		end)
		golapis.say("Timer 3 scheduled (0.03s delay, with async sleep)")

		golapis.say("Main thread done. Wait() will block until all timers complete.")
	`

	expected := "" +
		"Scheduling timers...\n" +
		"Timer 1 scheduled (0.05s delay)\n" +
		"Timer 2 scheduled (0.02s delay)\n" +
		"Timer 3 scheduled (0.03s delay, with async sleep)\n" +
		"Main thread done. Wait() will block until all timers complete.\n" +
		"[callback] Quick timer fired!\n" +
		"[callback] Async timer started, sleeping 0.01s...\n" +
		"[callback] Async timer done!\n" +
		"[callback] Timer fired! msg:hellocount:42\n"

	output, err := runLuaAndCapture(t, code)
	if err != nil {
		t.Fatalf("Lua error: %v", err)
	}
	if output != expected {
		t.Errorf("output mismatch\ngot:  %q\nwant: %q", output, expected)
	}
}
