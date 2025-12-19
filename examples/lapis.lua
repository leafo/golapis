_G.ngx = golapis
local lapis = require "lapis"

local app = lapis.Application()

app:match("/", function(self)
  return "Hello world!"
end)

require("lapis").serve(app)
