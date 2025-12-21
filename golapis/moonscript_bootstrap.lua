-- moonscript_bootstrap.lua
-- Receives entrypoint filename, installs moonloader, compiles and returns the chunk

local entrypoint = ...

-- Load moonscript (will error if not installed)
local moonscript = require("moonscript")

-- Install the moonloader so require() works with .moon files
require("moonscript.base").insert_loader()

-- Compile the entrypoint file and return the function
return moonscript.loadfile(entrypoint)
