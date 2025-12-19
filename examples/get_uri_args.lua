local args = golapis.req.get_uri_args()

local function format_value(v)
    if type(v) == "boolean" then
        return tostring(v)
    elseif type(v) == "table" then
        local parts = {}
        for _, item in ipairs(v) do
            table.insert(parts, format_value(item))
        end
        return "[" .. table.concat(parts, ", ") .. "]"
    else
        return '"' .. tostring(v) .. '"'
    end
end

if args == nil then
    golapis.print("No HTTP request context (CLI mode)")
else
    golapis.print("Query parameters:")
    for k, v in pairs(args) do
        golapis.print("  " .. k .. " = " .. format_value(v))
    end
end
