-- Test golapis.status for setting HTTP response status codes
-- Run with: go run main.go -http 8080 examples/set_status.lua
-- Then: curl -i http://localhost:8080

-- Read the default status (0 before any body is sent)
local initial_status = golapis.status
print("Initial status value: " .. tostring(initial_status))

-- Set a custom status code
golapis.status = 201
print("Status after setting to 201: " .. golapis.status)

-- You can change it multiple times before the response is sent
golapis.status = 404
print("Final status: " .. golapis.status)

-- Now output the body (this flushes headers - no more status changes after this)
golapis.say("You should see 'HTTP/1.1 404 Not Found' in the response headers")
golapis.say("Check with: curl -i http://localhost:8080")
