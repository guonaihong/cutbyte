package nghttp

import (
	_ "encoding/json"
	"fmt"
	"github.com/robertkrimen/otto"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Listen struct {
	Addr string
}

type Location struct {
	Reg   string
	Root  string
	Index []string
}

type Server struct {
	Listen     Listen
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

func (ng *NgMain) server(v interface{}) {
	servers := v.([]map[string]interface{})

	for _, ser := range servers {

		for _, l := range ser {
			listen, ok := l.(map[string]interface{})
			if !ok {
				return
			}

			server := Server{}
			for k, v := range listen {
				if k == "addr" {
					server.Listen.Addr = v.(string)
				}
				//fmt.Printf("k(%s),%T, v(%s), %T\n", k, k, v, v)
			}

			ng.Http.Server = append(ng.Http.Server, server)
		}
	}
}

func (ng *NgMain) http(v interface{}) {
	s, ok := v.(map[string]interface{})
	if !ok {
		return
	}

	for k, vv := range s {
		switch strings.ToLower(k) {
		case "server":
			ng.server(vv)
		}
	}
}

func (ng *NgMain) ngMain(call otto.FunctionCall) otto.Value {
	o, err := call.Argument(0).Export()
	if err != nil {
		return otto.Value{}
	}

	m := o.(map[string]interface{})

	for k, v := range m {
		fmt.Printf("k, %s\n", k)
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
		case "http":
			ng.http(v)
		}

	}

	return otto.Value{}
}

func (ng *NgMain) httpServerRun() {

	wg := sync.WaitGroup{}

	for _, v := range ng.Http.Server {
		wg.Add(1)
		go func(v Server) {
			defer wg.Done()
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("good !"))
			})

			fmt.Printf("-->%s\n", v.Listen.Addr)
			server := &http.Server{
				Addr:         v.Listen.Addr,
				ReadTimeout:  60 * time.Second,
				WriteTimeout: 60 * time.Second,
				Handler:      mux,
			}

			fmt.Println(server.ListenAndServe())
		}(v)
	}

	wg.Wait()
}

func Loop(conf string) {
	all, err := ioutil.ReadFile(conf)
	if err != nil {
		fmt.Printf("%s\n", err)
		return
	}

	ng := NgMain{}
	vm := otto.New()
	vm.Set("nghttp_main", ng.ngMain)
	_, err = vm.Run(string(all))
	if err != nil {
		fmt.Printf("%s\n", err)
	}

	fmt.Printf("%s\n", ng)
	ng.httpServerRun()
}
