package lua

import (
	"testing"
)

func TestLuaDnsTxt(t *testing.T) {
	luaScript := `
local module = {}

local dns = require "dns"
local string = require "string"

module.milterCheck = function(message)
    res, err = dns.queryTxt("google-public-dns-a.google.com")
    if err ~= nil then
	return "TEMPFAIL", err, 100
    end

    for k,v in pairs(res) do
        if string.lower(v) ~= "" then
 		return "PERMIT", res[k], 0
        end
    end

    return "ERROR", res[1], 0
end

return module
	`

	testLuaMilterCheck(t, luaScript, "http://xkcd.com/1361/", 0, 0)
}

func TestLuaDnsCname(t *testing.T) {
	luaScript := `
local module = {}

local dns = require "dns"
local string = require "string"

module.milterCheck = function(message)
    res, err = dns.queryCname("www.golang.org")
    if err ~= nil then
	return "TEMPFAIL", err, 100
    end

    for k,v in pairs(res) do
        if string.lower(v) ~= "" then
 		return "PERMIT", res[k], 0
        end
    end

    return "ERROR", res[1], 0
end

return module
	`

	testLuaMilterCheck(t, luaScript, "golang.org.", 0, 0)
}
