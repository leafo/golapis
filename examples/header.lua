-- Test golapis.header proxy object for setting response headers
-- Run with: go run main.go -http 8080 examples/header.lua
-- Then: curl -i http://localhost:8080

-- Set a custom header
golapis.header["X-Custom-Header"] = "hello world"

-- Set content type using underscore notation (converts to Content-Type)
golapis.header.content_type = "text/plain"

-- Set multiple cookies using a table
golapis.header["Set-Cookie"] = {
  "session=abc123; Path=/; HttpOnly",
  "theme=dark; Path=/"
}

-- Set cache control
golapis.header["Cache-Control"] = "no-cache, no-store, must-revalidate"

-- Now output the body (this flushes headers - no more header changes after this)
golapis.say("Hello from golapis!")
golapis.say("")
golapis.say("Check the response headers with: curl -i http://localhost:8080")
golapis.say("You should see X-Custom-Header, Content-Type, Set-Cookie, and Cache-Control")
