_G.ngx = golapis

if not package.preload.app then
  package.preload.app = function()
    local lapis = require "lapis"

    local app = lapis.Application()

    app:match("/", function(self)
      return "Hello world!"
    end)

    return app
  end
end

require("lapis").serve("app")
