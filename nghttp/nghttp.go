package nghttp

import (
	"fmt"
	"github.com/robertkrimen/otto"
	"io/ioutil"
	"strings"
)

type Listen struct {
	Addr string
}

type Server struct {
	Listen     string
	ServerName string
	AccessLog  string
}

type Http struct {
	Include     string
	DefaultType string
	AccessLog   string
	Server      []Server
}

type NgMain struct {
	ErrorLog string
	Pid      string
	Http     Http
}

func (ng *NgMain) ngMain(call otto.FunctionCall) otto.Value {
	o, err := call.Argument(0).Export()
	if err != nil {
		return otto.Value{}
	}

	m := o.(map[string]interface{})

	for k, v := range m {
		switch strings.ToLower(k) {
		case "error_log":
			errorLog, ok := v.(string)
			if ok {
				ng.ErrorLog = errorLog
			}
		case "pid":
			pid, ok := v.(string)
			if ok {
				ng.Pid = pid
			}
		}

	}

	return otto.Value{}
}

func Loop(conf string) {
	all, err := ioutil.ReadFile(conf)
	if err != nil {
		fmt.Printf("%s\n", err)
		return
	}

	ngMain := NgMain{}
	vm := otto.New()
	vm.Set("nghttp_main", ngMain)
	vm.Run(string(all))

}
