-- Test golapis.var proxy object

golapis.print("=== golapis.var test ===")
golapis.print("")

golapis.print("request_method:", golapis.var.request_method)
golapis.print("request_uri:", golapis.var.request_uri)
golapis.print("scheme:", golapis.var.scheme)
golapis.print("host:", golapis.var.host)
golapis.print("http_host:", golapis.var.http_host)
golapis.print("server_port:", golapis.var.server_port)
golapis.print("remote_addr:", golapis.var.remote_addr)
golapis.print("args:", golapis.var.args or "(none)")

golapis.print("")
golapis.print("=== HTTP headers via http_* ===")
golapis.print("http_user_agent:", golapis.var.http_user_agent or "(none)")
golapis.print("http_accept:", golapis.var.http_accept or "(none)")
golapis.print("http_referer:", golapis.var.http_referer or "(none)")

golapis.print("")
golapis.print("=== Unknown variable ===")
golapis.print("unknown_var:", golapis.var.unknown_var or "(nil)")
