local args = golapis.req.get_uri_args()

if args == nil then
    golapis.print("No HTTP request context (CLI mode)")
else
    golapis.print("Query parameters:")
    for k, v in pairs(args) do
        if type(v) == "table" then
            golapis.print("  " .. k .. " = [" .. table.concat(v, ", ") .. "]")
        else
            golapis.print("  " .. k .. " = " .. v)
        end
    end
end
