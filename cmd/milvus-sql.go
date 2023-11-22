// partially copied & changed from : https://github.com/flike/kingshard/blob/master/proxy/server/

// Copyright 2016 The kingshard Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"syscall"

	"github.com/flike/kingshard/core/golog"
	"github.com/haorenfsa/milvus-sql-proxy/pkg"
)

var configFile *string = flag.String("config", "./config.yaml", "config file")
var logLevel *string = flag.String("log-level", "info", "log level [debug|info|warn|error], default info")
var version *bool = flag.Bool("v", false, "the version of kingshard")

const (
	sqlLogName = "sql.log"
	sysLogName = "sys.log"
	MaxLogSize = 1024 * 1024 * 1024
)

var (
	BuildDate    string
	BuildVersion string
)

const banner string = `
████     ████ ██  ██                             ████████   ███████    ██      
░██░██   ██░██░░  ░██                            ██░░░░░░   ██░░░░░██  ░██      
░██░░██ ██ ░██ ██ ░██ ██    ██ ██   ██  ██████  ░██        ██     ░░██ ░██      
░██ ░░███  ░██░██ ░██░██   ░██░██  ░██ ██░░░░   ░█████████░██      ░██ ░██      
░██  ░░█   ░██░██ ░██░░██ ░██ ░██  ░██░░█████   ░░░░░░░░██░██    ██░██ ░██      
░██   ░    ░██░██ ░██ ░░████  ░██  ░██ ░░░░░██         ░██░░██  ░░ ██  ░██      
░██        ░██░██ ███  ░░██   ░░██████ ██████    ████████  ░░███████ ██░████████
░░         ░░ ░░ ░░░    ░░     ░░░░░░ ░░░░░░    ░░░░░░░░    ░░░░░░░ ░░ ░░░░░░░░       
`

func main() {
	fmt.Print(banner)
	runtime.GOMAXPROCS(runtime.NumCPU())
	flag.Parse()
	fmt.Printf("Git commit:%s\n", BuildVersion)
	fmt.Printf("Build time:%s\n", BuildDate)
	if *version {
		return
	}
	if len(*configFile) == 0 {
		fmt.Println("must use a config file")
		return
	}

	cfg, err := pkg.ParseConfigFile(*configFile)
	if err != nil {
		fmt.Printf("parse config file error:%v\n", err.Error())
		return
	}

	//when the log file size greater than 1GB, kingshard will generate a new file
	if len(cfg.LogPath) != 0 {
		sysFilePath := path.Join(cfg.LogPath, sysLogName)
		sysFile, err := golog.NewRotatingFileHandler(sysFilePath, MaxLogSize, 1)
		if err != nil {
			fmt.Printf("new log file error:%v\n", err.Error())
			return
		}
		golog.GlobalSysLogger = golog.New(sysFile, golog.Lfile|golog.Ltime|golog.Llevel)

		sqlFilePath := path.Join(cfg.LogPath, sqlLogName)
		sqlFile, err := golog.NewRotatingFileHandler(sqlFilePath, MaxLogSize, 1)
		if err != nil {
			fmt.Printf("new log file error:%v\n", err.Error())
			return
		}
		golog.GlobalSqlLogger = golog.New(sqlFile, golog.Lfile|golog.Ltime|golog.Llevel)
	}

	if *logLevel != "" {
		setLogLevel(*logLevel)
	} else {
		setLogLevel(cfg.LogLevel)
	}

	var svr *pkg.Server
	// var prometheusSvr *monitor.Prometheus
	svr, err = pkg.NewServer(cfg)
	if err != nil {
		golog.Error("main", "main", err.Error(), 0)
		golog.GlobalSysLogger.Close()
		golog.GlobalSqlLogger.Close()
		return
	}
	// prometheusSvr, err = monitor.NewPrometheus(cfg.PrometheusAddr, svr)
	// if err != nil {
	// 	golog.Error("main", "main", err.Error(), 0)
	// 	golog.GlobalSysLogger.Close()
	// 	golog.GlobalSqlLogger.Close()
	// 	svr.Close()
	// 	return
	// }

	sc := make(chan os.Signal, 1)
	signal.Notify(sc,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGPIPE,
		// syscall.SIGUSR1,
	)

	go func() {
		for {
			sig := <-sc
			switch sig {
			case syscall.SIGPIPE:
				golog.Info("main", "main", "Ignore broken pipe signal", 0)
			default:
				golog.Info("main", "main", "Got signal", 0, "signal", sig)
				golog.GlobalSysLogger.Close()
				golog.GlobalSqlLogger.Close()
				svr.Close()
			}
		}
	}()
	// go prometheusSvr.Run()
	golog.Info("main", "main", "starting server", 0)
	svr.Run()
}

func setLogLevel(level string) {
	switch strings.ToLower(level) {
	case "debug":
		golog.GlobalSysLogger.SetLevel(golog.LevelDebug)
	case "info":
		golog.GlobalSysLogger.SetLevel(golog.LevelInfo)
	case "warn":
		golog.GlobalSysLogger.SetLevel(golog.LevelWarn)
	case "error":
		golog.GlobalSysLogger.SetLevel(golog.LevelError)
	default:
		golog.GlobalSysLogger.SetLevel(golog.LevelError)
	}
}
