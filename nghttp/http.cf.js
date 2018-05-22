/*
var location4 = {
    "reg":"^/(images|javascript|js|css|flash|media|static)/",
    "root":"/var/www/virtual/htdocs",
    "expires": "30d",
}
*/

function Location(location) {
    this.reg = location.reg;
    if ('code' in location) {
        this.writeCode(location.code);
        return
    }
}

var locations = [
    new Location({reg:"^~ /helloworld", code:601}),
    new Location({reg:"/helloworld", code:602}),
    new Location({reg:"/helloworld", code:603}),
]

var servers = []
for (var i = 0; i < 3; i++) {
    servers.push({
        "listen" :{"addr": ":1234" + i},
        "location": locations[i],
    })
}

var error_page = {
    "code": "404",
    "to" :"/emtpy.gif",
};

var mysvr = [
    {"addr": "http://192.168.1.128:2000", "weight":5},
    {"addr": "http://192.168.1.128:2001", "weight":5},
    {"addr": "http://192.168.1.128:2002", "weight":5},
];

var location1 = {
    "reg" :"/",
    "root": "/root",
    "index": ["index.php", "index.html", "index.htm"],
    "proxy_pass": mysvr,
};

var server2 = {
    "listen": {"addr":":80"},
    "server_name" : "www.xxx.com",
    "access_log":"./local-access.log",
    "location":   [location1],
    "error_page": error_page,
}

servers.push(server2)

var http = {
    "include":"./mime.types",
    "default_type":  "application/octet-stream",
    "access_log":"./access.log",
    "server": servers,
};

var main = {
    "error_log" :"",
    "pid" : "/var/run/ng.pid",
    "http": http,
};

nghttp_main(main)
