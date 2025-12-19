-- Test golapis.var proxy object

golapis.say("=== golapis.var test ===")
golapis.say("")

golapis.say("request_method:", golapis.var.request_method)
golapis.say("request_uri:", golapis.var.request_uri)
golapis.say("scheme:", golapis.var.scheme)
golapis.say("host:", golapis.var.host)
golapis.say("http_host:", golapis.var.http_host)
golapis.say("server_port:", golapis.var.server_port)
golapis.say("remote_addr:", golapis.var.remote_addr)
golapis.say("args:", golapis.var.args or "(none)")

golapis.say("")
golapis.say("=== HTTP headers via http_* ===")
golapis.say("http_user_agent:", golapis.var.http_user_agent or "(none)")
golapis.say("http_accept:", golapis.var.http_accept or "(none)")
golapis.say("http_referer:", golapis.var.http_referer or "(none)")

golapis.say("")
golapis.say("=== Unknown variable ===")
golapis.say("unknown_var:", golapis.var.unknown_var or "(nil)")
