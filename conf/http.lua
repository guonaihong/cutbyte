--[[
function Location(location) 
    this.reg = location.reg;
    if location["code"] ~= nil then
        this.writeCode(location.code);
        return
    end
end
--]]

--[[
local locations = {
    new Location({reg:"^~ /helloworld", code:601}),
    new Location({reg:"/helloworld", code:602}),
    new Location({reg:"/helloworld", code:603}),
}
--]]

local servers = {}
for i = 0, 3 do
    table.insert(servers, {
        listen = {addr = ":1234"..i},
      --  "location": locations[i],
    })
end

local error_page = {
    code =  "404",
    to  = "/emtpy.gif",
};

local mysvr = {
    {addr = "http://192.168.1.128:2000", weight = 5},
    {addr = "http://192.168.1.128:2001", weight = 5},
    {addr = "http://192.168.1.128:2002", weight = 5},
}

local location1 = {
    reg = "/",
    root = "/root",
    index = {"index.php", "index.html", "index.htm"},
    proxy_pass =  mysvr,
};

local server2 = {
    listen =  {addr = ":80"},
    server_name = "www.xxx.com",
    access_log = "./local-access.log",
    location =   {location1},
    error_page =  error_page,
}

table.insert(servers, server2)

local http = {
    include = "./mime.types",
    default_type =  "application/octet-stream",
    access_log = "./access.log",
    server = servers,
};

local main = {
    error_log = "",
    pid = "/local/run/ng.pid",
    http =  http,
};

--cutbyte(main)
