var main = {
    "error_log" :"",
    "pid" :"/var/run/ng.pid",
};

var server = {
    "listen": { "addr":":80"}
            
}

var server1 = {
    "listen": { "addr":":80"}
}

var server2 = {
    "listen": { "addr":":80" }
}

var mysvr = [
    {"addr": "192.168.1.128:2000", "weight":5},
    {"addr": "192.168.1.128:2001", "weight":5},
    {"addr": "192.168.1.128:2002", "weight":5},
];

var http = {
    "include":"./mime.types",
    "default_type":  "application/octet-stream",
    "access_log":"./access.log;",
    "server":[
        server, server1, server2,
    ],
};
