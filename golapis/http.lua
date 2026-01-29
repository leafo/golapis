-- http.lua
-- LuaSocket-style HTTP client wrapper for golapis
--
-- TODO: Currently buffers entire request body and response into memory.
-- This means:
--   1. source iterator is fully consumed before sending (no chunked transfer encoding)
--   2. Response body is fully read before returning (no streaming to sink)
-- For large payloads, this could cause high memory usage. To support true
-- streaming, the Go implementation would need to accept an io.Reader for
-- request bodies and provide chunked response delivery to sinks.

local _M = {}

-- Pump source iterator to string
-- Returns: string on success, or nil, err on failure
local function pump_source(source)
    if not source then return nil end
    local chunks = {}
    while true do
        local chunk, err = source()
        if chunk then
            chunks[#chunks + 1] = chunk
        elseif err then
            return nil, err
        else
            break
        end
    end
    return table.concat(chunks)
end

-- Pump string to sink
-- Returns: true on success, or nil, err on failure
local function pump_to_sink(sink, data)
    if not sink then return true end
    local ok, err = sink(data)
    if not ok and err then
        return nil, err
    end
    ok, err = sink(nil)  -- signal end
    if not ok and err then
        return nil, err
    end
    return true
end

function _M.request(reqt, body)
    -- Handle simple form: request(url) or request(url, body)
    if type(reqt) == "string" then
        local url = reqt
        if body then
            reqt = {
                url = url,
                method = "POST",
                body = body,
                headers = { ["content-type"] = "application/x-www-form-urlencoded" }
            }
        else
            reqt = { url = url }
        end
    end

    -- Extract source and sink (don't mutate original reqt)
    local source = reqt.source
    local sink = reqt.sink

    -- Build request table for Go (copy relevant fields, don't mutate original)
    local go_reqt = {
        url = reqt.url,
        method = reqt.method,
        body = reqt.body,
        headers = reqt.headers,
        redirect = reqt.redirect,
        maxredirects = reqt.maxredirects,
        timeout = reqt.timeout,
    }

    -- Handle source: pump to body string
    if source then
        local body_data, err = pump_source(source)
        if err then
            return nil, err
        end
        go_reqt.body = body_data
        if go_reqt.body and not (go_reqt.headers and go_reqt.headers["content-length"]) then
            -- Copy headers to avoid mutating original
            local new_headers = {}
            if go_reqt.headers then
                for k, v in pairs(go_reqt.headers) do
                    new_headers[k] = v
                end
            end
            new_headers["content-length"] = tostring(#go_reqt.body)
            go_reqt.headers = new_headers
        end
    end

    -- Call Go implementation
    local resp_body, status, headers, statusline = golapis.http._request(go_reqt)

    if not resp_body then
        return nil, status  -- error case: status is error message
    end

    -- Pump to sink if provided
    if sink then
        local ok, err = pump_to_sink(sink, resp_body)
        if not ok then
            return nil, err
        end
        return 1, status, headers, statusline
    end

    return resp_body, status, headers, statusline
end

return _M
